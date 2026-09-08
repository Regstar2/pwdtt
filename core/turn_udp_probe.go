package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/turn/v5"
)

const (
	defaultTurnUDPProbeTimeout = 45 * time.Second
	turnUDPProbeReadLimit      = 4096
)

var turnUDPProbeSequence atomic.Int64

type TurnUDPProbeConfig struct {
	Hash      string
	Target    string
	QueryName string
	Timeout   time.Duration
}

type TurnUDPProbeResult struct {
	TurnAddress  string
	RelayAddress string
	Target       string
	Source       string
	QueryName    string
	ResponseBytes int
	AnswerCount  uint16
	RCode        uint8
}

// ProbeTurnUDP checks whether VK TURN can relay UDP directly to an arbitrary
// external peer. The probe sends one DNS A query through the TURN allocation
// and validates the DNS response transaction ID and status.
//
// It does not start WireGuard and does not contact a wdtt-server.
func ProbeTurnUDP(parent context.Context, cfg TurnUDPProbeConfig) (TurnUDPProbeResult, error) {
	var result TurnUDPProbeResult

	if parent == nil {
		parent = context.Background()
	}

	hash := strings.TrimSpace(cfg.Hash)
	if hash == "" {
		return result, fmt.Errorf("VK hash is required")
	}

	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		target = "1.1.1.1:53"
	}
	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return result, fmt.Errorf("resolve UDP target %q: %w", target, err)
	}
	if targetAddr.IP == nil {
		return result, fmt.Errorf("UDP target %q did not resolve to an IP address", target)
	}

	queryName := strings.TrimSpace(cfg.QueryName)
	if queryName == "" {
		queryName = "example.com"
	}
	query, transactionID, err := buildDNSAQuery(queryName)
	if err != nil {
		return result, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTurnUDPProbeTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	streamID := int(990000 + turnUDPProbeSequence.Add(1))
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

		result, err = probeTurnUDPAtEndpoint(
			ctx,
			turnAddr,
			turnUser,
			turnPass,
			targetAddr,
			queryName,
			query,
			transactionID,
		)
		if err == nil {
			return result, nil
		}
		attempts = append(attempts, fmt.Errorf("%s: %w", turnAddr, err))
	}

	if len(attempts) == 0 {
		return result, fmt.Errorf("VK returned no usable TURN endpoints")
	}
	return result, fmt.Errorf("all TURN UDP probe attempts failed: %w", errors.Join(attempts...))
}

func probeTurnUDPAtEndpoint(
	ctx context.Context,
	turnAddr string,
	username string,
	password string,
	target *net.UDPAddr,
	queryName string,
	query []byte,
	transactionID uint16,
) (TurnUDPProbeResult, error) {
	result := TurnUDPProbeResult{
		TurnAddress: turnAddr,
		Target:      target.String(),
		QueryName:   queryName,
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

	if _, err := relay.WriteTo(query, target); err != nil {
		return result, fmt.Errorf("send DNS query to %s through TURN: %w", target, err)
	}

	buf := make([]byte, turnUDPProbeReadLimit)
	n, source, err := relay.ReadFrom(buf)
	if err != nil {
		return result, fmt.Errorf("read DNS response through TURN: %w", err)
	}
	result.Source = source.String()
	result.ResponseBytes = n

	answerCount, rcode, err := validateDNSResponse(buf[:n], transactionID)
	if err != nil {
		return result, fmt.Errorf("validate DNS response from %s: %w", source, err)
	}
	result.AnswerCount = answerCount
	result.RCode = rcode

	return result, nil
}

func buildDNSAQuery(name string) ([]byte, uint16, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" {
		return nil, 0, fmt.Errorf("DNS query name is required")
	}

	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, 0, fmt.Errorf("generate DNS transaction ID: %w", err)
	}
	transactionID := binary.BigEndian.Uint16(idBytes[:])

	query := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(query[0:2], transactionID)
	binary.BigEndian.PutUint16(query[2:4], 0x0100)
	binary.BigEndian.PutUint16(query[4:6], 1)

	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return nil, 0, fmt.Errorf("invalid DNS query name %q", name)
		}
		if len(label) > 63 {
			return nil, 0, fmt.Errorf("DNS label is too long in %q", name)
		}
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0)
	query = append(query, 0, 1)
	query = append(query, 0, 1)

	if len(query) > 255 {
		return nil, 0, fmt.Errorf("DNS query name is too long")
	}
	return query, transactionID, nil
}

func validateDNSResponse(response []byte, expectedID uint16) (uint16, uint8, error) {
	if len(response) < 12 {
		return 0, 0, fmt.Errorf("response is too short: %d bytes", len(response))
	}
	if binary.BigEndian.Uint16(response[0:2]) != expectedID {
		return 0, 0, fmt.Errorf("transaction ID mismatch")
	}

	flags := binary.BigEndian.Uint16(response[2:4])
	if flags&0x8000 == 0 {
		return 0, 0, fmt.Errorf("packet is not a DNS response")
	}

	rcode := uint8(flags & 0x000f)
	if rcode != 0 {
		return 0, rcode, fmt.Errorf("DNS server returned rcode=%d", rcode)
	}

	answerCount := binary.BigEndian.Uint16(response[6:8])
	if answerCount == 0 {
		return 0, rcode, fmt.Errorf("DNS response contains no answers")
	}

	return answerCount, rcode, nil
}
