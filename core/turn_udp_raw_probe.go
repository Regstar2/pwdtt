package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/turn/v5"
)

const (
	defaultTurnUDPRawProbeTimeout     = 45 * time.Second
	defaultTurnUDPRawProbeReadTimeout = 5 * time.Second
	maxTurnUDPRawProbePayload         = 1200
	turnUDPRawPreviewBytes            = 64
)

var turnUDPRawProbeSequence atomic.Int64

// TurnUDPRawProbeConfig describes a one-datagram UDP egress diagnostic.
// Payload is intentionally capped to avoid fragmentation and accidental bulk traffic.
type TurnUDPRawProbeConfig struct {
	Hash        string
	Target      string
	Payload     []byte
	Timeout     time.Duration
	ReadTimeout time.Duration
}

// TurnUDPRawAttempt records every stage separately for one VK/OK TURN endpoint.
// TURN credentials are deliberately never included.
type TurnUDPRawAttempt struct {
	TurnAddress        string
	RelayAddress       string
	Target             string
	Allocated          bool
	SentBytes          int
	ResponseSource     string
	ResponseBytes      int
	ResponseHexPreview string
	ReceiveTimedOut    bool
	Error              string
}

// TurnUDPRawProbeResult aggregates all TURN endpoint attempts.
type TurnUDPRawProbeResult struct {
	Attempts     []TurnUDPRawAttempt
	AnyAllocated bool
	AnySent      bool
	AnyResponse  bool
}

// ProbeTurnUDPRaw allocates UDP relay(s) on VK/OK TURN, sends exactly one
// caller-supplied datagram to the target per TURN endpoint, then waits for one
// response datagram.
//
// A receive timeout is diagnostic data, not an allocation/send failure. This
// makes it possible to distinguish "TURN could not send" from "packet was sent
// but the peer did not reply".
func ProbeTurnUDPRaw(parent context.Context, cfg TurnUDPRawProbeConfig) (TurnUDPRawProbeResult, error) {
	var result TurnUDPRawProbeResult

	if parent == nil {
		parent = context.Background()
	}

	hash := strings.TrimSpace(cfg.Hash)
	if hash == "" {
		return result, fmt.Errorf("VK hash is required")
	}

	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		return result, fmt.Errorf("UDP target is required")
	}
	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return result, fmt.Errorf("resolve UDP target %q: %w", target, err)
	}
	if targetAddr.IP == nil {
		return result, fmt.Errorf("UDP target %q did not resolve to an IP address", target)
	}

	if len(cfg.Payload) == 0 {
		return result, fmt.Errorf("UDP payload is required")
	}
	if len(cfg.Payload) > maxTurnUDPRawProbePayload {
		return result, fmt.Errorf("UDP payload is too large: %d bytes (max %d)", len(cfg.Payload), maxTurnUDPRawProbePayload)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTurnUDPRawProbeTimeout
	}
	readTimeout := cfg.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = defaultTurnUDPRawProbeReadTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	streamID := int(998000 + turnUDPRawProbeSequence.Add(1))
	turnUser, turnPass, turnURLs, err := GetCreds(ctx, hash, streamID)
	if err != nil {
		return result, fmt.Errorf("get VK TURN credentials: %w", err)
	}
	if len(turnURLs) == 0 {
		return result, fmt.Errorf("VK returned no TURN endpoints")
	}

	for _, candidate := range turnURLs {
		turnAddr := strings.TrimSpace(candidate)
		if turnAddr == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			result.Attempts = append(result.Attempts, TurnUDPRawAttempt{
				TurnAddress: turnAddr,
				Target:      targetAddr.String(),
				Error:       err.Error(),
			})
			break
		}

		attempt := probeTurnUDPRawAtEndpoint(
			ctx,
			turnAddr,
			turnUser,
			turnPass,
			targetAddr,
			cfg.Payload,
			readTimeout,
		)
		result.Attempts = append(result.Attempts, attempt)
		result.AnyAllocated = result.AnyAllocated || attempt.Allocated
		result.AnySent = result.AnySent || attempt.SentBytes > 0
		result.AnyResponse = result.AnyResponse || attempt.ResponseBytes > 0
	}

	if len(result.Attempts) == 0 {
		return result, errors.New("VK returned no usable TURN endpoints")
	}
	return result, nil
}

func probeTurnUDPRawAtEndpoint(
	ctx context.Context,
	turnAddr string,
	username string,
	password string,
	target *net.UDPAddr,
	payload []byte,
	readTimeout time.Duration,
) TurnUDPRawAttempt {
	attempt := TurnUDPRawAttempt{
		TurnAddress: turnAddr,
		Target:      target.String(),
	}

	serverAddr, err := net.ResolveUDPAddr("udp", turnAddr)
	if err != nil {
		attempt.Error = fmt.Sprintf("resolve TURN UDP endpoint: %v", err)
		return attempt
	}

	controlConn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		attempt.Error = fmt.Sprintf("connect to TURN over UDP: %v", err)
		return attempt
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
		attempt.Error = fmt.Sprintf("create TURN client: %v", err)
		return attempt
	}
	defer client.Close()

	if err := client.Listen(); err != nil {
		attempt.Error = fmt.Sprintf("TURN listen: %v", err)
		return attempt
	}

	relay, err := client.Allocate()
	if err != nil {
		attempt.Error = fmt.Sprintf("Allocate UDP: %v", err)
		return attempt
	}
	defer relay.Close()

	attempt.Allocated = true
	attempt.RelayAddress = relay.LocalAddr().String()

	deadline := time.Now().Add(readTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := relay.SetDeadline(deadline); err != nil {
		attempt.Error = fmt.Sprintf("set relay deadline: %v", err)
		return attempt
	}

	n, err := relay.WriteTo(payload, target)
	if err != nil {
		attempt.Error = fmt.Sprintf("send UDP datagram to %s through TURN: %v", target, err)
		return attempt
	}
	attempt.SentBytes = n

	buf := make([]byte, turnUDPProbeReadLimit)
	n, source, err := relay.ReadFrom(buf)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			attempt.ReceiveTimedOut = true
			return attempt
		}
		if ctx.Err() != nil {
			attempt.Error = ctx.Err().Error()
			return attempt
		}
		attempt.Error = fmt.Sprintf("read UDP response through TURN: %v", err)
		return attempt
	}

	attempt.ResponseSource = source.String()
	attempt.ResponseBytes = n
	attempt.ResponseHexPreview = turnUDPRawHexPreview(buf[:n])
	return attempt
}

func turnUDPRawHexPreview(data []byte) string {
	if len(data) > turnUDPRawPreviewBytes {
		data = data[:turnUDPRawPreviewBytes]
	}
	return hex.EncodeToString(data)
}
