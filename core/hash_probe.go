package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

type HashProbeStatus string
type HashProbeErrorType string

const (
	HashProbeValid   HashProbeStatus = "valid"
	HashProbeInvalid HashProbeStatus = "invalid"
	HashProbeError   HashProbeStatus = "error"

	HashProbeErrorInvalidHash HashProbeErrorType = "invalid_hash"
	HashProbeErrorNetwork     HashProbeErrorType = "network"
	HashProbeErrorVK          HashProbeErrorType = "vk"
	HashProbeErrorTURN        HashProbeErrorType = "turn"
	HashProbeErrorDTLS        HashProbeErrorType = "dtls"
	HashProbeErrorWRAP        HashProbeErrorType = "wrap"
	HashProbeErrorCanceled    HashProbeErrorType = "canceled"
	HashProbeErrorSetup       HashProbeErrorType = "setup"
	HashProbeErrorTimeout     HashProbeErrorType = "timeout"
)

type HashProbeProgress struct {
	Stage     string
	State     string
	Message   string
	ElapsedMs int64
	Attempt   int
}

type HashProbeConfig struct {
	PeerAddr   string
	Password   string
	Hash       string
	DeviceID   string
	TurnHost   string
	TurnPort   string
	ObfsMode   string
	OnProgress func(HashProbeProgress)
}

type HashProbeResult struct {
	Status    HashProbeStatus
	ErrorType HashProbeErrorType
	Message   string
	LatencyMs int64
}

var hashProbeSequence atomic.Int64

func ProbeHash(parent context.Context, cfg HashProbeConfig) HashProbeResult {
	started := time.Now()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if strings.TrimSpace(cfg.Hash) == "" {
		return hashProbeErrorResult(HashProbeErrorSetup, "VK hash is empty", started)
	}
	if strings.TrimSpace(cfg.PeerAddr) == "" {
		return hashProbeErrorResult(HashProbeErrorSetup, "peer address is empty", started)
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return hashProbeErrorResult(HashProbeErrorSetup, "server password is empty", started)
	}

	emitHashProbeProgress(cfg, started, "dns", "running", "Разрешение адреса сервера", 0)
	peer, err := net.ResolveUDPAddr("udp", strings.TrimSpace(cfg.PeerAddr))
	if err != nil {
		emitHashProbeProgress(cfg, started, "dns", "error", "Адрес сервера не разрешён", 0)
		return hashProbeErrorResult(HashProbeErrorNetwork, "server address could not be resolved", started)
	}
	emitHashProbeProgress(cfg, started, "dns", "success", "Адрес сервера разрешён", 0)
	wrapKey, err := deriveWrapKey(cfg.Password)
	if err != nil {
		return hashProbeErrorResult(HashProbeErrorSetup, "WRAP key could not be derived", started)
	}

	streamID := int(900000 + hashProbeSequence.Add(1))
	emitHashProbeProgress(cfg, started, "credentials", "running", "Получение VK-кредов", 0)
	turnUser, turnPass, turnURLs, err := fetchVkCredsSerialized(ctx, cfg.Hash, streamID)
	if err != nil {
		result := classifyHashProbeCredentialError(err, started)
		emitHashProbeProgress(cfg, started, "credentials", "error", result.Message, 0)
		return result
	}
	if len(turnURLs) == 0 {
		emitHashProbeProgress(cfg, started, "credentials", "error", "VK не вернул TURN endpoints", 0)
		return hashProbeErrorResult(HashProbeErrorVK, "VK returned no TURN endpoints", started)
	}
	emitHashProbeProgress(cfg, started, "credentials", "success", "VK-креды получены", 0)

	localConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return hashProbeErrorResult(HashProbeErrorSetup, "probe UDP socket could not be opened", started)
	}
	stopSocket := context.AfterFunc(ctx, func() { _ = localConn.Close() })

	stats := NewStats()
	dispatcher := NewDispatcher(ctx, localConn, stats)
	defer func() {
		cancel()
		stopSocket()
		_ = localConn.Close()
		dispatcher.Shutdown()
	}()

	_, localPort, err := net.SplitHostPort(localConn.LocalAddr().String())
	if err != nil || localPort == "" {
		return hashProbeErrorResult(HashProbeErrorSetup, "probe UDP port could not be determined", started)
	}
	deviceID := strings.TrimSpace(cfg.DeviceID)
	if deviceID == "" {
		deviceID = "hash-probe"
	}
	obfsMode := strings.TrimSpace(cfg.ObfsMode)
	if obfsMode == "" {
		obfsMode = "audio"
	}
	tp := &TurnParams{
		Host:     cfg.TurnHost,
		Port:     cfg.TurnPort,
		Hashes:   []string{cfg.Hash},
		WrapKey:  wrapKey,
		ObfsMode: obfsMode,
		Trace: func(_ int, stage, state, message string, duration time.Duration) {
			progress := HashProbeProgress{
				Stage: stage, State: state, Message: message,
				ElapsedMs: elapsedMilliseconds(started),
			}
			if duration > 0 {
				progress.Message = fmt.Sprintf("%s (%d ms)", message, duration.Milliseconds())
			}
			if cfg.OnProgress != nil {
				cfg.OnProgress(progress)
			}
		},
	}

	var lastSessionErr *SessionError
	for index, turnAddr := range turnURLs {
		if ctx.Err() != nil {
			return classifyHashProbeContextError(ctx.Err(), started)
		}
		emitHashProbeProgress(cfg, started, "turn", "running", "Проверка TURN endpoint", index+1)
		sessionCtx, sessionCancel := context.WithCancel(ctx)
		done := make(chan *SessionError, 1)
		sessionID := 910000 + index
		go func() {
			_, sessionErr := RunSession(
				sessionCtx, tp, peer, dispatcher, localPort, false, nil,
				sessionID, turnAddr, turnUser, turnPass, streamID,
				deviceID, cfg.Password, stats,
			)
			done <- sessionErr
		}()

		ticker := time.NewTicker(50 * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				if stats.ActiveConnections.Load() > 0 {
					ticker.Stop()
					sessionCancel()
					cancel()
					emitHashProbeProgress(cfg, started, "completed", "success", "VK-хеш прошёл functional probe", index+1)
					emitHashProbeProgress(cfg, started, "completed", "success", "VK-хеш прошёл functional probe", index+1)
					return HashProbeResult{
						Status:    HashProbeValid,
						Message:   "VK hash completed VK/TURN/WRAP/DTLS probe",
						LatencyMs: elapsedMilliseconds(started),
					}
				}
			case sessionErr := <-done:
				ticker.Stop()
				sessionCancel()
				if stats.ActiveConnections.Load() > 0 {
					cancel()
					return HashProbeResult{
						Status:    HashProbeValid,
						Message:   "VK hash completed VK/TURN/WRAP/DTLS probe",
						LatencyMs: elapsedMilliseconds(started),
					}
				}
				lastSessionErr = sessionErr
				if sessionErr != nil && sessionErr.Type == SessionErrorAddressDead {
					break
				}
				return classifyHashProbeSessionError(sessionErr, started)
			case <-ctx.Done():
				ticker.Stop()
				sessionCancel()
				return classifyHashProbeContextError(ctx.Err(), started)
			}
		}
	}

	return classifyHashProbeSessionError(lastSessionErr, started)
}

func classifyHashProbeCredentialError(err error, started time.Time) HashProbeResult {
	var callErr *CallUnavailableError
	if errors.As(err, &callErr) {
		return HashProbeResult{
			Status:    HashProbeInvalid,
			ErrorType: HashProbeErrorInvalidHash,
			Message:   "VK call is unavailable",
			LatencyMs: elapsedMilliseconds(started),
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return classifyHashProbeContextError(err, started)
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "captcha") || strings.Contains(lower, "vk api") || strings.Contains(lower, "vkcalls") {
		return hashProbeErrorResult(HashProbeErrorVK, "VK credential probe failed", started)
	}
	return hashProbeErrorResult(HashProbeErrorNetwork, "VK credential request failed", started)
}

func classifyHashProbeSessionError(err *SessionError, started time.Time) HashProbeResult {
	if err == nil {
		return hashProbeErrorResult(HashProbeErrorTURN, "TURN/DTLS probe ended before a session became active", started)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err.Err, context.Canceled) {
		return classifyHashProbeContextError(context.Canceled, started)
	}
	switch err.Type {
	case SessionErrorWrapTimeout:
		return hashProbeErrorResult(HashProbeErrorWRAP, "WRAP/DTLS handshake timed out", started)
	case SessionErrorAddressDead:
		if strings.Contains(strings.ToLower(err.Error()), "dtls") {
			return hashProbeErrorResult(HashProbeErrorDTLS, "DTLS session could not be established", started)
		}
		return hashProbeErrorResult(HashProbeErrorTURN, "TURN endpoint is unavailable", started)
	case SessionErrorFatal:
		return hashProbeErrorResult(HashProbeErrorDTLS, "probe session failed", started)
	default:
		return hashProbeErrorResult(HashProbeErrorTURN, "probe session failed", started)
	}
}

func classifyHashProbeContextError(err error, started time.Time) HashProbeResult {
	if errors.Is(err, context.Canceled) {
		return hashProbeErrorResult(HashProbeErrorCanceled, "VK hash check was canceled", started)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return hashProbeErrorResult(HashProbeErrorTimeout, "VK hash check timed out", started)
	}
	return hashProbeErrorResult(HashProbeErrorNetwork, "VK hash check failed", started)
}

func emitHashProbeProgress(cfg HashProbeConfig, started time.Time, stage, state, message string, attempt int) {
	if cfg.OnProgress == nil {
		return
	}
	cfg.OnProgress(HashProbeProgress{
		Stage: stage, State: state, Message: message,
		ElapsedMs: elapsedMilliseconds(started), Attempt: attempt,
	})
}

func hashProbeErrorResult(errorType HashProbeErrorType, message string, started time.Time) HashProbeResult {
	return HashProbeResult{
		Status:    HashProbeError,
		ErrorType: errorType,
		Message:   message,
		LatencyMs: elapsedMilliseconds(started),
	}
}

func elapsedMilliseconds(started time.Time) int64 {
	ms := time.Since(started).Milliseconds()
	if ms < 1 {
		return 1
	}
	return ms
}

func (r HashProbeResult) Error() error {
	if r.Status != HashProbeError && r.Status != HashProbeInvalid {
		return nil
	}
	return fmt.Errorf("%s: %s", r.ErrorType, r.Message)
}
