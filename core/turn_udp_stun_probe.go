package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/stun/v3"
	"github.com/pion/turn/v5"
)

const defaultTurnUDPSTUNProbeTimeout = 45 * time.Second

var turnUDPSTUNProbeSequence atomic.Int64

type TurnUDPSTUNProbeConfig struct {
	Hash    string
	Target  string
	Timeout time.Duration
}

type TurnUDPSTUNProbeResult struct {
	TurnAddress   string
	RelayAddress  string
	Target        string
	ResponseSource string
	MappedAddress string
	ResponseBytes int
}

func ProbeTurnUDPSTUN(parent context.Context, cfg TurnUDPSTUNProbeConfig) (TurnUDPSTUNProbeResult, error) {
	var result TurnUDPSTUNProbeResult

	if parent == nil {
		parent = context.Background()
	}

	hash := strings.TrimSpace(cfg.Hash)
	if hash == "" {
		return result, fmt.Errorf("VK hash is required")
	}

	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		target = "stun.cloudflare.com:3478"
	}
	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return result, fmt.Errorf("resolve STUN target %q: %w", target, err)
	}
	if targetAddr.IP == nil {
		return result, fmt.Errorf("STUN target %q did not resolve to an IP address", target)
	}

	request, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return result, fmt.Errorf("build STUN binding request: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTurnUDPSTUNProbeTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	streamID := int(995000 + turnUDPSTUNProbeSequence.Add(1))
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

		result, err = probeTurnUDPSTUNAtEndpoint(
			ctx,
			turnAddr,
			turnUser,
			turnPass,
			targetAddr,
			request,
		)
		if err == nil {
			return result, nil
		}
		attempts = append(attempts, fmt.Errorf("%s: %w", turnAddr, err))
	}

	if len(attempts) == 0 {
		return result, fmt.Errorf("VK returned no usable TURN endpoints")
	}
	return result, fmt.Errorf("all TURN UDP STUN probe attempts failed: %w", errors.Join(attempts...))
}

func probeTurnUDPSTUNAtEndpoint(
	ctx context.Context,
	turnAddr string,
	username string,
	password string,
	target *net.UDPAddr,
	request *stun.Message,
) (TurnUDPSTUNProbeResult, error) {
	result := TurnUDPSTUNProbeResult{
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

	if _, err := relay.WriteTo(request.Raw, target); err != nil {
		return result, fmt.Errorf("send STUN binding request to %s through TURN: %w", target, err)
	}

	buf := make([]byte, turnUDPProbeReadLimit)
	n, source, err := relay.ReadFrom(buf)
	if err != nil {
		return result, fmt.Errorf("read STUN response through TURN: %w", err)
	}
	result.ResponseSource = source.String()
	result.ResponseBytes = n

	response := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
	if err := response.Decode(); err != nil {
		return result, fmt.Errorf("decode STUN response from %s: %w", source, err)
	}
	if response.TransactionID != request.TransactionID {
		return result, fmt.Errorf("STUN transaction ID mismatch")
	}
	if response.Type != stun.BindingSuccess {
		return result, fmt.Errorf("unexpected STUN response type: %s", response.Type)
	}

	var mapped stun.XORMappedAddress
	if err := mapped.GetFrom(response); err != nil {
		return result, fmt.Errorf("read XOR-MAPPED-ADDRESS: %w", err)
	}
	result.MappedAddress = mapped.String()

	return result, nil
}
