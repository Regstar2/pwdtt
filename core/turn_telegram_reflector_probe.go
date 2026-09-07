package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/turn/v5"
)

const defaultTelegramReflectorProbeTimeout = 45 * time.Second

var telegramReflectorProbeSequence atomic.Int64

type TelegramReflectorProbeConfig struct {
	Hash       string
	Target     string
	PeerTagHex string
	Timeout    time.Duration
}

type TelegramReflectorProbeResult struct {
	TurnAddress    string
	RelayAddress   string
	Target         string
	ResponseSource string
	ResponseBytes  int
}

func ProbeTelegramReflector(parent context.Context, cfg TelegramReflectorProbeConfig) (TelegramReflectorProbeResult, error) {
	var result TelegramReflectorProbeResult

	if parent == nil {
		parent = context.Background()
	}

	hash := strings.TrimSpace(cfg.Hash)
	if hash == "" {
		return result, fmt.Errorf("VK hash is required")
	}

	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		return result, fmt.Errorf("Telegram reflector target is required")
	}
	if looksLikePlaceholder(target) {
		return result, fmt.Errorf("Telegram reflector target is still a placeholder: %q", target)
	}

	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return result, fmt.Errorf("resolve Telegram reflector target %q: %w", target, err)
	}
	if targetAddr.IP == nil {
		return result, fmt.Errorf("Telegram reflector target %q did not resolve to an IP address", target)
	}

	hello, peerTagPrefix, err := buildTelegramReflectorHello(cfg.PeerTagHex)
	if err != nil {
		return result, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTelegramReflectorProbeTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	streamID := int(997000 + telegramReflectorProbeSequence.Add(1))
	turnUser, turnPass, turnURLs, err := GetCreds(ctx, hash, streamID)
	if err != nil {
		return result, fmt.Errorf("get VK TURN credentials: %w", err)
	}
	if len(turnURLs) == 0 {
		return result, fmt.Errorf("VK returned no TURN endpoints")
	}

	var attempts []error
	for _, candidate := range turnURLs {
		if err := ctx.Err(); err != nil {
			attempts = append(attempts, err)
			break
		}

		turnAddr := strings.TrimSpace(candidate)
		if turnAddr == "" {
			continue
		}

		result, err = probeTelegramReflectorAtEndpoint(
			ctx,
			turnAddr,
			turnUser,
			turnPass,
			targetAddr,
			hello,
			peerTagPrefix,
		)
		if err == nil {
			return result, nil
		}
		attempts = append(attempts, fmt.Errorf("%s: %w", turnAddr, err))
	}

	if len(attempts) == 0 {
		return result, fmt.Errorf("VK returned no usable TURN endpoints")
	}
	return result, fmt.Errorf("all Telegram reflector probe attempts failed: %w", errors.Join(attempts...))
}

func probeTelegramReflectorAtEndpoint(
	ctx context.Context,
	turnAddr string,
	username string,
	password string,
	target *net.UDPAddr,
	hello []byte,
	peerTagPrefix []byte,
) (TelegramReflectorProbeResult, error) {
	result := TelegramReflectorProbeResult{
		TurnAddress: turnAddr,
		Target:      target.String(),
	}

	serverAddr, err := net.ResolveUDPAddr("udp", turnAddr)
	if err != nil {
		return result, fmt.Errorf("resolve TURN UDP endpoint: %w", err)
	}

	controlConn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return result, fmt.Errorf("connect to TURN over UDP: %w", err)
	}
	defer controlConn.Close()
	_ = controlConn.SetReadBuffer(socketBufSize)
	_ = controlConn.SetWriteBuffer(socketBufSize)

	stopOnCancel := context.AfterFunc(ctx, func() {
		_ = controlConn.Close()
	})
	defer stopOnCancel()

	var turnConn net.PacketConn = &connectedUDPConn{controlConn}
	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         turnAddr,
		TURNServerAddr:         turnAddr,
		Conn:                   turnConn,
		Username:               username,
		Password:               password,
		RequestedAddressFamily: turn.RequestedAddressFamilyIPv4,
		LoggerFactory:          &NullLoggerFactory{},
	})
	if err != nil {
		return result, fmt.Errorf("create TURN client: %w", err)
	}
	defer client.Close()

	if err := client.Listen(); err != nil {
		return result, fmt.Errorf("TURN listen: %w", err)
	}

	relay, err := client.Allocate()
	if err != nil {
		return result, fmt.Errorf("Allocate UDP: %w", err)
	}
	defer relay.Close()

	result.RelayAddress = relay.LocalAddr().String()

	deadline := time.Now().Add(10 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := relay.SetDeadline(deadline); err != nil {
		return result, fmt.Errorf("set relay deadline: %w", err)
	}

	if _, err := relay.WriteTo(hello, target); err != nil {
		return result, fmt.Errorf("send Telegram reflector hello to %s through TURN: %w", target, err)
	}

	buf := make([]byte, turnUDPProbeReadLimit)
	for {
		n, source, err := relay.ReadFrom(buf)
		if err != nil {
			return result, fmt.Errorf("read Telegram reflector response through TURN: %w", err)
		}
		if !sameUDPAddr(source, target) {
			continue
		}
		if n < 16 {
			return result, fmt.Errorf("Telegram reflector response is too short: %d bytes", n)
		}
		if !equalBytes(buf[:12], peerTagPrefix) {
			return result, fmt.Errorf("Telegram reflector response peer_tag prefix mismatch")
		}

		result.ResponseSource = source.String()
		result.ResponseBytes = n
		return result, nil
	}
}

// buildTelegramReflectorHello accepts either the 12-byte peer_tag prefix that
// tgcalls actually uses, or the original 16-byte phoneConnection peer_tag.
// Current tgcalls discards the last four bytes and replaces them with a fresh
// local random tag before sending the reflector hello.
func buildTelegramReflectorHello(peerTagHex string) ([]byte, []byte, error) {
	peerTagHex = strings.TrimSpace(peerTagHex)
	if peerTagHex == "" {
		return nil, nil, fmt.Errorf("Telegram peer_tag prefix is required")
	}
	if looksLikePlaceholder(peerTagHex) {
		return nil, nil, fmt.Errorf("Telegram peer_tag is still a placeholder")
	}

	peerTag, err := hex.DecodeString(peerTagHex)
	if err != nil {
		return nil, nil, fmt.Errorf("decode Telegram peer_tag hex: %w", err)
	}
	if len(peerTag) != 12 && len(peerTag) != 16 {
		return nil, nil, fmt.Errorf("Telegram peer_tag must be a 12-byte prefix (24 hex characters) or full 16 bytes (32 hex characters), got %d bytes", len(peerTag))
	}
	peerTagPrefix := peerTag[:12]

	localTag := make([]byte, 4)
	for {
		if _, err := rand.Read(localTag); err != nil {
			return nil, nil, fmt.Errorf("generate Telegram reflector local tag: %w", err)
		}
		if localTag[0] != 0 || localTag[1] != 0 || localTag[2] != 0 || localTag[3] != 0 {
			break
		}
	}

	packet := make([]byte, 0, 40)
	packet = append(packet, peerTagPrefix...)
	packet = append(packet, localTag...)
	for i := 0; i < 12; i++ {
		packet = append(packet, 0xff)
	}
	packet = append(packet, 0xfe, 0xff, 0xff, 0xff)

	var pingValue [8]byte
	binary.BigEndian.PutUint64(pingValue[:], 123)
	packet = append(packet, pingValue[:]...)

	prefix := append([]byte(nil), peerTagPrefix...)
	return packet, prefix, nil
}

func looksLikePlaceholder(value string) bool {
	return strings.Contains(value, "<") || strings.Contains(value, ">")
}

func sameUDPAddr(addr net.Addr, expected *net.UDPAddr) bool {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	return udpAddr.Port == expected.Port && udpAddr.IP.Equal(expected.IP)
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
