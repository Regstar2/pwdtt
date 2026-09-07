package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/turn/v5"
)

const (
	defaultTelegramMTProtoUDPProbeTimeout = 45 * time.Second
	telegramMTProtoPerModeTimeout         = 3 * time.Second
)

var telegramMTProtoUDPProbeSequence atomic.Int64

type TelegramMTProtoUDPProbeConfig struct {
	Hash    string
	Target  string
	Timeout time.Duration
}

type TelegramMTProtoUDPProbeResult struct {
	TurnAddress    string
	RelayAddress   string
	Target         string
	ResponseSource string
	Mode           string
	ResponseBytes  int
}

type telegramMTProtoUDPCandidate struct {
	mode   string
	packet []byte
	nonce  [16]byte
}

// ProbeTelegramMTProtoUDP checks whether a Telegram DC accepts an ordinary
// unauthenticated MTProto req_pq_multi exchange over UDP when the datagram is
// relayed through VK/OK TURN.
//
// No Telegram account, auth key, API ID, or wdtt-server is used.
func ProbeTelegramMTProtoUDP(parent context.Context, cfg TelegramMTProtoUDPProbeConfig) (TelegramMTProtoUDPProbeResult, error) {
	var result TelegramMTProtoUDPProbeResult

	if parent == nil {
		parent = context.Background()
	}

	hash := strings.TrimSpace(cfg.Hash)
	if hash == "" {
		return result, fmt.Errorf("VK hash is required")
	}

	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		target = "149.154.167.51:443"
	}
	if strings.Contains(target, "<") || strings.Contains(target, ">") {
		return result, fmt.Errorf("Telegram target still contains a placeholder: %q", target)
	}
	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return result, fmt.Errorf("resolve Telegram UDP target %q: %w", target, err)
	}
	if targetAddr.IP == nil {
		return result, fmt.Errorf("Telegram UDP target %q did not resolve to an IP address", target)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTelegramMTProtoUDPProbeTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	streamID := int(998000 + telegramMTProtoUDPProbeSequence.Add(1))
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

		result, err = probeTelegramMTProtoUDPAtEndpoint(
			ctx,
			turnAddr,
			turnUser,
			turnPass,
			targetAddr,
		)
		if err == nil {
			return result, nil
		}
		attempts = append(attempts, fmt.Errorf("%s: %w", turnAddr, err))
	}

	if len(attempts) == 0 {
		return result, fmt.Errorf("VK returned no usable TURN endpoints")
	}
	return result, fmt.Errorf("all Telegram MTProto UDP probe attempts failed: %w", errors.Join(attempts...))
}

func probeTelegramMTProtoUDPAtEndpoint(
	ctx context.Context,
	turnAddr string,
	username string,
	password string,
	target *net.UDPAddr,
) (TelegramMTProtoUDPProbeResult, error) {
	result := TelegramMTProtoUDPProbeResult{
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

	candidates, err := buildTelegramMTProtoUDPCandidates(time.Now())
	if err != nil {
		return result, err
	}

	var modeErrors []error
	buf := make([]byte, turnUDPProbeReadLimit)

	for _, probe := range candidates {
		if err := ctx.Err(); err != nil {
			modeErrors = append(modeErrors, err)
			break
		}

		deadline := time.Now().Add(telegramMTProtoPerModeTimeout)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		if err := relay.SetDeadline(deadline); err != nil {
			return result, fmt.Errorf("set relay deadline: %w", err)
		}

		if _, err := relay.WriteTo(probe.packet, target); err != nil {
			modeErrors = append(modeErrors, fmt.Errorf("%s send: %w", probe.mode, err))
			continue
		}

		for {
			n, source, err := relay.ReadFrom(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					modeErrors = append(modeErrors, fmt.Errorf("%s: no response", probe.mode))
					break
				}
				modeErrors = append(modeErrors, fmt.Errorf("%s read: %w", probe.mode, err))
				break
			}

			if validateTelegramResPQ(buf[:n], probe.nonce) {
				result.ResponseSource = source.String()
				result.ResponseBytes = n
				result.Mode = probe.mode
				return result, nil
			}

			// A datagram came back through the TURN permission, but it wasn't
			// the req_pq_multi response for this candidate. Keep listening until
			// the per-mode deadline in case the valid response follows.
			result.ResponseSource = source.String()
			result.ResponseBytes = n
		}
	}

	if len(modeErrors) == 0 {
		return result, fmt.Errorf("no MTProto UDP framing candidates were tested")
	}
	return result, fmt.Errorf("Telegram DC returned no valid resPQ: %w", errors.Join(modeErrors...))
}

func buildTelegramMTProtoUDPCandidates(now time.Time) ([]telegramMTProtoUDPCandidate, error) {
	modes := []string{
		"raw",
		"abridged",
		"abridged-init",
		"intermediate",
		"intermediate-init",
		"padded-intermediate-init",
		"full",
	}

	result := make([]telegramMTProtoUDPCandidate, 0, len(modes))
	for _, mode := range modes {
		payload, nonce, err := buildTelegramReqPQMulti(now)
		if err != nil {
			return nil, err
		}
		packet, err := wrapTelegramMTProtoUDP(mode, payload)
		if err != nil {
			return nil, err
		}
		result = append(result, telegramMTProtoUDPCandidate{
			mode:   mode,
			packet: packet,
			nonce:  nonce,
		})
	}
	return result, nil
}

func buildTelegramReqPQMulti(now time.Time) ([]byte, [16]byte, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nonce, fmt.Errorf("generate MTProto nonce: %w", err)
	}

	var fractional [4]byte
	if _, err := rand.Read(fractional[:]); err != nil {
		return nil, nonce, fmt.Errorf("generate MTProto message ID: %w", err)
	}

	messageID := (uint64(now.Unix()) << 32) | uint64(binary.LittleEndian.Uint32(fractional[:])&^3)

	payload := make([]byte, 40)
	// auth_key_id = 0 for unencrypted MTProto messages.
	binary.LittleEndian.PutUint64(payload[8:16], messageID)
	binary.LittleEndian.PutUint32(payload[16:20], 20)
	binary.LittleEndian.PutUint32(payload[20:24], 0xbe7e8ef1) // req_pq_multi
	copy(payload[24:40], nonce[:])

	return payload, nonce, nil
}

func wrapTelegramMTProtoUDP(mode string, payload []byte) ([]byte, error) {
	switch mode {
	case "raw":
		return append([]byte(nil), payload...), nil

	case "abridged":
		if len(payload)%4 != 0 || len(payload)/4 >= 127 {
			return nil, fmt.Errorf("payload cannot use short abridged envelope")
		}
		packet := make([]byte, 1, 1+len(payload))
		packet[0] = byte(len(payload) / 4)
		return append(packet, payload...), nil

	case "abridged-init":
		if len(payload)%4 != 0 || len(payload)/4 >= 127 {
			return nil, fmt.Errorf("payload cannot use short abridged envelope")
		}
		packet := []byte{0xef, byte(len(payload) / 4)}
		return append(packet, payload...), nil

	case "intermediate":
		packet := make([]byte, 4, 4+len(payload))
		binary.LittleEndian.PutUint32(packet[:4], uint32(len(payload)))
		return append(packet, payload...), nil

	case "intermediate-init":
		packet := []byte{0xee, 0xee, 0xee, 0xee, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(packet[4:8], uint32(len(payload)))
		return append(packet, payload...), nil

	case "padded-intermediate-init":
		packet := []byte{0xdd, 0xdd, 0xdd, 0xdd, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(packet[4:8], uint32(len(payload)))
		return append(packet, payload...), nil

	case "full":
		packet := make([]byte, 8, 12+len(payload))
		binary.LittleEndian.PutUint32(packet[0:4], uint32(len(payload)+12))
		binary.LittleEndian.PutUint32(packet[4:8], 0)
		packet = append(packet, payload...)
		checksum := crc32.ChecksumIEEE(packet)
		var crc [4]byte
		binary.LittleEndian.PutUint32(crc[:], checksum)
		packet = append(packet, crc[:]...)
		return packet, nil

	default:
		return nil, fmt.Errorf("unsupported MTProto UDP framing mode %q", mode)
	}
}

func validateTelegramResPQ(datagram []byte, expectedNonce [16]byte) bool {
	const (
		resPQConstructor = uint32(0x05162463)
		plainHeaderSize  = 20
	)

	for offset := plainHeaderSize; offset+20 <= len(datagram); offset++ {
		if binary.LittleEndian.Uint32(datagram[offset:offset+4]) != resPQConstructor {
			continue
		}
		if !equalBytes(datagram[offset+4:offset+20], expectedNonce[:]) {
			continue
		}

		header := datagram[offset-plainHeaderSize : offset]
		if binary.LittleEndian.Uint64(header[0:8]) != 0 {
			continue
		}
		bodyLen := binary.LittleEndian.Uint32(header[16:20])
		if bodyLen < 20 {
			continue
		}
		return true
	}
	return false
}
