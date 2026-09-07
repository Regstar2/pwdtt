package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/turn/v5"
)

const (
	defaultTurnTCPProbeTimeout = 45 * time.Second
	turnTCPProbeReadLimit      = 4096
)

var turnTCPProbeSequence atomic.Int64

// TurnTCPProbeConfig describes a one-shot RFC 6062 capability probe.
// Payload is optional: when empty, a successful relayed TCP connection is enough.
type TurnTCPProbeConfig struct {
	Hash    string
	Target  string
	Payload []byte
	Timeout time.Duration
}

// TurnTCPProbeResult contains only non-secret diagnostic data.
type TurnTCPProbeResult struct {
	TurnAddress     string
	RelayAddress    string
	Target          string
	ResponsePreview string
}

// ProbeTurnTCP checks whether VK TURN credentials can be used for an RFC 6062
// TCP allocation and an outgoing TCP connection to cfg.Target.
//
// It does not start WireGuard, does not contact a wdtt-server and never logs
// TURN credentials.
func ProbeTurnTCP(parent context.Context, cfg TurnTCPProbeConfig) (TurnTCPProbeResult, error) {
	var result TurnTCPProbeResult

	if parent == nil {
		parent = context.Background()
	}

	hash := strings.TrimSpace(cfg.Hash)
	if hash == "" {
		return result, fmt.Errorf("VK hash is required")
	}

	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		return result, fmt.Errorf("target is required")
	}

	targetAddr, err := net.ResolveTCPAddr("tcp", target)
	if err != nil {
		return result, fmt.Errorf("resolve target %q: %w", target, err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTurnTCPProbeTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	streamID := int(980000 + turnTCPProbeSequence.Add(1))
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

		result, err = probeTurnTCPAtEndpoint(ctx, turnAddr, turnUser, turnPass, targetAddr, cfg.Payload)
		if err == nil {
			return result, nil
		}

		attempts = append(attempts, fmt.Errorf("%s: %w", turnAddr, err))
	}

	if len(attempts) == 0 {
		return result, fmt.Errorf("VK returned no usable TURN endpoints")
	}

	return result, fmt.Errorf("all TURN TCP probe attempts failed: %w", errors.Join(attempts...))
}

func probeTurnTCPAtEndpoint(
	ctx context.Context,
	turnAddr string,
	username string,
	password string,
	target *net.TCPAddr,
	payload []byte,
) (TurnTCPProbeResult, error) {
	result := TurnTCPProbeResult{
		TurnAddress: turnAddr,
		Target:      target.String(),
	}

	serverAddr, err := net.ResolveTCPAddr("tcp", turnAddr)
	if err != nil {
		return result, fmt.Errorf("resolve TURN TCP endpoint: %w", err)
	}

	controlConn, err := net.DialTCP("tcp", nil, serverAddr)
	if err != nil {
		return result, fmt.Errorf("connect to TURN over TCP: %w", err)
	}
	defer controlConn.Close()

	stopOnCancel := context.AfterFunc(ctx, func() {
		_ = controlConn.Close()
	})
	defer stopOnCancel()

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: turnAddr,
		TURNServerAddr: turnAddr,
		Conn:           turn.NewSTUNConn(controlConn),
		Username:       username,
		Password:       password,
		LoggerFactory:  &NullLoggerFactory{},
	})
	if err != nil {
		return result, fmt.Errorf("create TURN client: %w", err)
	}
	defer client.Close()

	if err := client.Listen(); err != nil {
		return result, fmt.Errorf("TURN listen: %w", err)
	}

	allocation, err := client.AllocateTCP()
	if err != nil {
		return result, fmt.Errorf("AllocateTCP: %w", err)
	}
	defer allocation.Close()

	result.RelayAddress = allocation.Addr().String()

	relayedConn, err := allocation.DialTCP("tcp", nil, target)
	if err != nil {
		return result, fmt.Errorf("DialTCP %s: %w", target, err)
	}
	defer relayedConn.Close()

	if len(payload) == 0 {
		return result, nil
	}

	deadline := time.Now().Add(10 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := relayedConn.SetDeadline(deadline); err != nil {
		return result, fmt.Errorf("set relayed connection deadline: %w", err)
	}

	if _, err := relayedConn.Write(payload); err != nil {
		return result, fmt.Errorf("write probe payload: %w", err)
	}

	buf := make([]byte, turnTCPProbeReadLimit)
	n, err := relayedConn.Read(buf)
	if err != nil {
		return result, fmt.Errorf("read probe response: %w", err)
	}
	result.ResponsePreview = string(buf[:n])

	return result, nil
}
