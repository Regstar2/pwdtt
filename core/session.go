package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/turn/v5"
)

const (
	workerSendBuf      = 128
	sessionReadTimeout = 30 * time.Minute
	readBufSize        = 1600
	socketBufSize      = 625 * 1024
	keepaliveByte      = 0xFF
	keepaliveInterval  = 15 * time.Second
)

var handshakeSem = make(chan struct{}, 3)

type connectedUDPConn struct{ *net.UDPConn }

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }

// dtlsRelayTraffic captures packet counts only. Payload, credentials and peer IP
// are deliberately excluded from diagnostics and exported reports.
type dtlsRelayTraffic struct {
	sent       atomic.Int64
	received   atomic.Int64
	fromPeer   atomic.Int64
	accepted   atomic.Int64
	rejected   atomic.Int64
}

func (m *dtlsRelayTraffic) hint() string {
	switch {
	case m.sent.Load() == 0:
		return "no_outbound_relay_packets"
	case m.received.Load() == 0:
		return "no_inbound_relay_packets"
	case m.fromPeer.Load() == 0:
		return "no_inbound_from_expected_peer"
	case m.accepted.Load() == 0:
		return "inbound_wrap_not_verified"
	default:
		return "inbound_forwarded_to_dtls"
	}
}

func (m *dtlsRelayTraffic) summary(peer *net.UDPAddr, mode string) string {
	family := "ipv6"
	if peer.IP.To4() != nil {
		family = "ipv4"
	}
	return fmt.Sprintf("peer_family=%s peer_port=%d obfs_mode=%s sent=%d received=%d from_peer=%d accepted=%d rejected=%d observation=%s",
		family, peer.Port, mode, m.sent.Load(), m.received.Load(), m.fromPeer.Load(),
		m.accepted.Load(), m.rejected.Load(), m.hint())
}

func isDTLSTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return true
	}
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deadline") || strings.Contains(message, "timeout")
}

// SessionErrorType — тип ошибки сессии
type SessionErrorType string

const (
	// ADDRESS_DEAD — этот конкретный TURN-адрес не работает (квота, unreachable, timeout)
	SessionErrorAddressDead SessionErrorType = "ADDRESS_DEAD"
	// WRAP_TIMEOUT — таймаут DTLS; TURN Allocate мог быть успешным, причина пока неизвестна
	SessionErrorWrapTimeout SessionErrorType = "WRAP_TIMEOUT"
	// FATAL — невосстановимая ошибка (FATAL_AUTH, хеш мёртв и т.д.)
	SessionErrorFatal SessionErrorType = "FATAL"
)

// SessionError — структурированная ошибка сессии
type SessionError struct {
	Type    SessionErrorType
	Address string // TURN-адрес, на котором произошла ошибка (если применимо)
	Err     error
}

func (e *SessionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v (addr=%s)", e.Type, e.Err, e.Address)
	}
	return fmt.Sprintf("%s (addr=%s)", e.Type, e.Address)
}

func (e *SessionError) Unwrap() error { return e.Err }

func classifyTurnAllocateError(err error) SessionErrorType {
	if err == nil {
		return SessionErrorFatal
	}

	errLower := strings.ToLower(err.Error())
	addressDeadMarkers := []string{
		"quota",
		"486",
		"unreachable",
		"timeout",
		"connection refused",
		"no route to host",
		"all retransmissions failed",
	}
	for _, marker := range addressDeadMarkers {
		if strings.Contains(errLower, marker) {
			return SessionErrorAddressDead
		}
	}
	return SessionErrorFatal
}

// RunSession устанавливает TURN-сессию с указанным адресом.
// В отличие от предыдущей версии, адрес передаётся явно, а не выбирается по индексу.
func RunSession(
	ctx context.Context,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	sessionID int,
	turnAddr string, // конкретный TURN-адрес
	turnUser string, // username для TURN
	turnPass string, // password для TURN
	cacheStreamID int, // для handleAuthError
	deviceID, password string,
	stats *Stats,
) (bool, *SessionError) {
	configDelivered := false

	if turnAddr == "" {
		return false, &SessionError{
			Type: SessionErrorFatal,
			Err:  fmt.Errorf("пустой TURN-адрес"),
		}
	}

	urlhost, urlport, err := net.SplitHostPort(turnAddr)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("разбор TURN-адреса: %w", err),
		}
	}
	if tp.Host != "" {
		urlhost = tp.Host
	}
	if tp.Port != "" {
		urlport = tp.Port
	}
	turnAddrResolved := net.JoinHostPort(urlhost, urlport)

	resolved, err := net.ResolveUDPAddr("udp", turnAddrResolved)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("резолв TURN: %w", err),
		}
	}

	c, err := net.DialUDP("udp", nil, resolved)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("подключение TURN UDP: %w", err),
		}
	}
	defer c.Close()
	_ = c.SetReadBuffer(socketBufSize)
	_ = c.SetWriteBuffer(socketBufSize)
	var turnConn net.PacketConn = &connectedUDPConn{c}

	log.Printf("[СЕССИЯ #%d] TURN UDP (%s)", sessionID, turnAddr)

	var addrFamily turn.RequestedAddressFamily
	if peer.IP.To4() != nil {
		addrFamily = turn.RequestedAddressFamilyIPv4
	} else {
		addrFamily = turn.RequestedAddressFamilyIPv6
	}

	tc, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         turnAddrResolved,
		TURNServerAddr:         turnAddrResolved,
		Conn:                   turnConn,
		Username:               turnUser,
		Password:               turnPass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          &NullLoggerFactory{},
	})
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN клиент: %w", err),
		}
	}
	defer tc.Close()

	if err = tc.Listen(); err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN Listen: %w", err),
		}
	}

	turnStarted := time.Now()
	emitConnectionProgress("turn", "running", "Выделение TURN relay")
	traceSession(tp, sessionID, "turn", "running", "TURN Allocate", 0)
	relay, err := tc.Allocate()
	if err != nil {
		traceSession(tp, sessionID, "turn", "error", "TURN Allocate failed", time.Since(turnStarted))
		if isAuthError(err) {
			handleAuthError(cacheStreamID)
		}

		return false, &SessionError{
			Type:    classifyTurnAllocateError(err),
			Address: turnAddr,
			Err:     fmt.Errorf("TURN Allocate: %w", err),
		}
	}
	defer relay.Close()
	emitConnectionProgress("turn", "success", "TURN relay готов")
	traceSession(tp, sessionID, "turn", "success", "TURN relay ready", time.Since(turnStarted))

	getStreamCache(cacheStreamID).errorCount.Store(0)

	log.Printf("[СЕССИЯ #%d] Relay: %s", sessionID, relay.LocalAddr())

	pipeA, pipeB := connutil.AsyncPacketPipe()

	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	var sessionWg sync.WaitGroup
	sessionWg.Add(1)
	go func() {
		defer sessionWg.Done()
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				tc.SendBindingRequest()
			}
		}
	}()

	var relayWg sync.WaitGroup
	relayWg.Add(2)
	var relayTraffic dtlsRelayTraffic

	useWrap := len(tp.WrapKey) == wrapKeyLen

	var obfsCfg *ObfsConfig
	var obfsWriteState *ObfsState
	if useWrap {
		wrapStarted := time.Now()
		emitConnectionProgress("wrap", "running", "Настройка WRAP")
		traceSession(tp, sessionID, "wrap", "running", "Configure WRAP", 0)
		obfsCfg = NewObfsConfig(tp.ObfsMode)
		obfsWriteState = NewObfsState()
		emitConnectionProgress("wrap", "success", "WRAP настроен")
		traceSession(tp, sessionID, "wrap", "success", "WRAP configured", time.Since(wrapStarted))
	}

	stopRelay := context.AfterFunc(sessCtx, func() {
		_ = relay.SetDeadline(time.Now())
		_ = pipeA.SetDeadline(time.Now())
	})
	defer stopRelay()

	go func() {
		defer relayWg.Done()
		defer sessCancel()

		readBufLen := readBufSize + 80
		buf := make([]byte, readBufLen)
		plain := make([]byte, readBufSize)
		for {
			n, from, readErr := relay.ReadFrom(buf)
			if readErr != nil {
				return
			}
			relayTraffic.received.Add(1)
			if src, ok := from.(*net.UDPAddr); ok && src.Port == peer.Port && src.IP.Equal(peer.IP) {
				relayTraffic.fromPeer.Add(1)
			}
			payload := buf[:n]
			if useWrap {
				if !obfsIsRTPPacket(payload) {
					relayTraffic.rejected.Add(1)
					continue
				}
				m, wrapErr := obfsUnwrapPacket(tp.WrapKey, payload, plain)
				if wrapErr != nil {
					relayTraffic.rejected.Add(1)
					continue
				}
				payload = plain[:m]
			}
			relayTraffic.accepted.Add(1)
			if _, writeErr := pipeA.WriteTo(payload, peer); writeErr != nil {
				return
			}
		}
	}()

	go func() {
		defer relayWg.Done()
		defer sessCancel()
		b := make([]byte, readBufSize)
		for {
			n, _, readErr := pipeA.ReadFrom(b)
			if readErr != nil {
				return
			}
			out := b[:n]
			if useWrap {
				if obfsCfg != nil && obfsWriteState != nil {
					wrapped, wrapErr := obfsWrapPacket(tp.WrapKey, out, obfsCfg, obfsWriteState)
					if wrapErr != nil {
						log.Printf("[СЕССИЯ #%d] OBFS wrap: %v", sessionID, wrapErr)
						return
					}
					out = wrapped
				}
			}
			if _, writeErr := relay.WriteTo(out, peer); writeErr != nil {
				return
			}
			relayTraffic.sent.Add(1)
		}
	}()

	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     fmt.Errorf("генерация сертификата: %w", err),
		}
	}

	select {
	case handshakeSem <- struct{}{}:
	case <-sessCtx.Done():
		return false, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     sessCtx.Err(),
		}
	}

	dtlsCfg := &dtls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
	}

	dtlsConn, err := dtls.Client(pipeB, peer, dtlsCfg)
	if err != nil {
		<-handshakeSem
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("DTLS клиент: %w", err),
		}
	}
	defer dtlsConn.Close()

	hctx, hcancel := context.WithTimeout(sessCtx, 20*time.Second)
	dtlsStarted := time.Now()
	emitConnectionProgress("dtls", "running", "Проверка DTLS")
	traceSession(tp, sessionID, "dtls", "running", "DTLS handshake", 0)
	log.Printf("[ВОРКЕР #%d] [DTLS] Рукопожатие (Handshake)...", sessionID)
	err = dtlsConn.HandshakeContext(hctx)
	hcancel()
	<-handshakeSem

	// These are observations, not proof of a server, network or key defect.
	// Never include peer IP, packet contents or WRAP key in exported diagnostics.
	traceSession(tp, sessionID, "dtls_transport", "observed", relayTraffic.summary(peer, tp.ObfsMode), time.Since(dtlsStarted))
	if err != nil {
		if useWrap && isDTLSTimeout(err) {
			stats.DTLSHandshakeTimeouts.Add(1)
			traceSession(tp, sessionID, "dtls", "error", "DTLS handshake timed out", time.Since(dtlsStarted))
			emitConnectionProgress("dtls", "warning", "DTLS не подтвердился: проверьте peer, UDP и WRAP; причина пока неизвестна")
			emitConnectionProgress("wrap", "warning", "WRAP не подтверждён сервером")
			return false, &SessionError{
				Type:    SessionErrorWrapTimeout,
				Address: turnAddr,
				Err:     fmt.Errorf("DTLS timeout: подтверждение peer не получено (причина не установлена): %w", err),
			}
		}
		traceSession(tp, sessionID, "dtls", "error", "DTLS handshake failed", time.Since(dtlsStarted))
		emitConnectionProgress("dtls", "warning", "DTLS-сессия не установлена: проверьте диагностику")
		return false, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     fmt.Errorf("DTLS handshake error (TURN status unconfirmed): %w", err),
		}
	}
	stats.EverEstablished.Store(true)
	emitConnectionProgress("dtls", "success", "DTLS-сессия установлена")
	traceSession(tp, sessionID, "dtls", "success", "DTLS session established", time.Since(dtlsStarted))
	emitConnectionProgress("workers", "running", "Запуск рабочих потоков")
	log.Printf("[ВОРКЕР #%d] [DTLS] Соединение установлено ✓", sessionID)

	stats.ActiveConnections.Add(1)
	defer stats.ActiveConnections.Add(-1)

	if getConfig && configCh != nil {
		conf, confErr := RequestConfig(dtlsConn, localPort, deviceID, password)
		if confErr != nil {
			errStr := confErr.Error()
			if strings.Contains(errStr, "FATAL_AUTH") {
				emitConnectionProgress("vpn", "error", "Сервер отклонил пароль или конфигурацию")
				return false, &SessionError{
					Type:    SessionErrorFatal,
					Address: turnAddr,
					Err:     confErr,
				}
			}
			log.Printf("[ВОРКЕР #%d] Ошибка конфига: %v", sessionID, confErr)
		} else if conf != "" {
			select {
			case configCh <- conf:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] Конфиг получен", sessionID)
			default:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] Конфиг уже был доставлен другим воркером", sessionID)
			}
		} else {
			log.Printf("[ВОРКЕР #%d] Сервер ещё не выдал WireGuard-конфиг, повторим позже", sessionID)
		}
	} else {
		if authErr := SendAuth(dtlsConn, deviceID, password); authErr != nil {
			log.Printf("[ВОРКЕР #%d] Ошибка авторизации: %v", sessionID, authErr)
		}
	}

	emitReady()

	slot := &WorkerSlot{
		ID:     sessionID,
		SendCh: make(chan []byte, workerSendBuf),
	}
	d.Register(slot)
	emitConnectionProgress("workers", "success", "Рабочие потоки запущены")
	defer d.Unregister(slot)

	var proxyWg sync.WaitGroup
	proxyWg.Add(3)

	stopDTLS := context.AfterFunc(sessCtx, func() {
		_ = dtlsConn.SetDeadline(time.Now())
	})
	defer stopDTLS()

	go func() {
		defer proxyWg.Done()
		t := time.NewTicker(keepaliveInterval)
		defer t.Stop()
		ping := []byte{keepaliveByte}
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				_ = dtlsConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := dtlsConn.Write(ping); err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		for {
			select {
			case <-sessCtx.Done():
				return
			case pkt, ok := <-slot.SendCh:
				if !ok {
					return
				}
				_ = dtlsConn.SetWriteDeadline(time.Now().Add(sessionReadTimeout))
				_, writeErr := dtlsConn.Write(pkt)
				putPktBuf(pkt)
				if writeErr != nil {
					log.Printf("[ВОРКЕР #%d] Ошибка Writer: %v", sessionID, writeErr)
					return
				}
			}
		}
	}()

	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		for {
			pkt := getPktBuf(2048)
			_ = dtlsConn.SetReadDeadline(time.Now().Add(sessionReadTimeout))
			n, readErr := dtlsConn.Read(pkt)
			if readErr != nil {
				putPktBuf(pkt)
				if sessCtx.Err() != nil {
					return
				}
				if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
					continue
				}
				log.Printf("[ВОРКЕР #%d] Ошибка Reader: %v", sessionID, readErr)
				return
			}

			if n == 1 && pkt[0] == keepaliveByte {
				putPktBuf(pkt)
				continue
			}

			pkt = pkt[:n]
			select {
			case d.ReturnCh <- pkt:
			case <-sessCtx.Done():
				putPktBuf(pkt)
				return
			}
		}
	}()

	proxyWg.Wait()
	sessCancel()
	relayWg.Wait()
	sessionWg.Wait()
	_ = pipeA.Close()
	_ = pipeB.Close()
	log.Printf("[СЕССИЯ #%d] Завершена", sessionID)
	return configDelivered, nil
}

func traceSession(tp *TurnParams, sessionID int, stage, state, message string, duration time.Duration) {
	if tp == nil || tp.Trace == nil {
		return
	}
	tp.Trace(sessionID, stage, state, message, duration)
}
