package backend

import (
	"context"
	"fmt"
	"log"
	"sync"

	core "wg-turn-client"
)

// Bridge — мост между App.go и ядром.
type Bridge struct {
	ctx                context.Context
	store              *Store
	onEvent            func(name string, args ...any)
	mu                 sync.Mutex
	core               *core.Core
	running            bool
	wgApplied          bool
	connectedPublished bool
	logFile            *LogFile // current or last session, retained after disconnect
	logWarningSent     bool
}

func NewBridge(ctx context.Context, store *Store, onEvent func(string, ...any)) *Bridge {
	return &Bridge{ctx: ctx, store: store, onEvent: onEvent}
}

// Connect подключается к серверу.
func (b *Bridge) Connect(params ConnectParams) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		log.Printf("[BRIDGE] Connect rejected: already running")
		return fmt.Errorf("already running")
	}
	// Сразу помечаем как running чтобы заблокировать параллельные вызовы
	b.running = true
	b.wgApplied = false
	b.connectedPublished = false
	b.mu.Unlock()

	// resetRunning сбрасывает флаг при любой ошибке до запуска forwardEvents
	resetRunning := func() {
		b.mu.Lock()
		b.running = false
		b.wgApplied = false
		b.connectedPublished = false
		if b.logFile != nil {
			_ = b.logFile.Close()
		}
		b.mu.Unlock()
	}

	hashes := params.Hashes
	if len(hashes) == 0 {
		resetRunning()
		return fmt.Errorf("нет хешей VK")
	}

	workers := params.Workers
	if workers <= 0 {
		workers = 24
	}

	deviceID := params.DeviceID
	if deviceID == "" {
		deviceID = "unknown"
	}

	settings := b.store.LoadSettings()
	cfg := core.Config{
		PeerAddr:    params.PeerAddr,
		Password:    params.Password,
		Hashes:      hashes,
		DeviceID:    deviceID,
		Workers:     workers,
		CaptchaMode: params.CaptchaMode,
		ObfsMode:    params.ObfsMode,
		Fingerprint: params.Fingerprint,
		OperationID: params.OperationID,
		DebugLogging: settings.DebugLogging,
	}

	session := b.store.OpenLogFile()
	b.mu.Lock()
	b.logFile = session
	b.logWarningSent = session.Warning() != ""
	b.mu.Unlock()
	if warning := session.Warning(); warning != "" {
		b.onEvent("log", "WARN", warning)
	}

	c := core.New(cfg)
	events, err := c.Start()
	if err != nil {
		log.Printf("[BRIDGE] core start failed: %v", err)
		resetRunning()
		return fmt.Errorf("core start: %w", err)
	}

	b.mu.Lock()
	b.core = c
	b.running = true
	b.wgApplied = false
	b.connectedPublished = false
	b.mu.Unlock()

	go b.forwardEvents(events)
	return nil
}

func (b *Bridge) Disconnect() {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()

	// Tear down WireGuard interface immediately — don't wait for core shutdown
	wg.Teardown()
	log.Printf("[BRIDGE] WG интерфейс снят по запросу пользователя")

	if c != nil {
		c.Stop()
	}
}

func (b *Bridge) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

func (b *Bridge) SetDebugLogging(enabled bool) {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()
	if c != nil {
		c.SetDebugLogging(enabled)
	}
}

func (b *Bridge) SendCaptchaResult(token string) {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()
	if c != nil {
		c.SolveCaptcha(token)
	}
}

func tunnelTrafficConfirmed(active int32, bytesUp, bytesDown int64) bool {
	return active > 0 && bytesUp > 0 && bytesDown > 0
}

// SnapshotSessionLog returns the current or most recently closed journal.
func (b *Bridge) SnapshotSessionLog() (sessionLogSnapshot, bool) {
	b.mu.Lock()
	session := b.logFile
	b.mu.Unlock()
	if session == nil {
		return sessionLogSnapshot{}, false
	}
	return session.Snapshot(), true
}

func (b *Bridge) recordLog(level, message string, emitUI bool) {
	message = sanitizeDiagnosticText(message)
	var warning string
	b.mu.Lock()
	if b.logFile != nil {
		if err := b.logFile.Write(level, message); err != nil && !b.logWarningSent {
			warning = b.logFile.Warning()
			b.logWarningSent = true
		}
	}
	b.mu.Unlock()
	if emitUI {
		b.onEvent("log", level, message)
	}
	if warning != "" {
		b.onEvent("log", "WARN", warning)
	}
}

// forwardEvents читает канал событий ядра и пробрасывает в Wails.
func (b *Bridge) forwardEvents(events <-chan core.Event) {
	for ev := range events {
		switch ev.Type {
		case core.EventState:
			b.onEvent("state_changed", ev.Status)

		case core.EventLog:
			b.recordLog(ev.Level, ev.Message, ev.Level != "SKIP")

		case core.EventStats:
			b.onEvent("stats", map[string]any{
				"active":     ev.Active,
				"bytes_up":   ev.BytesUp,
				"bytes_down": ev.BytesDown,
			})

			b.mu.Lock()
			publishConnected := b.wgApplied && !b.connectedPublished && tunnelTrafficConfirmed(ev.Active, ev.BytesUp, ev.BytesDown)
			if publishConnected {
				b.connectedPublished = true
			}
			b.mu.Unlock()
			if publishConnected {
				b.recordLog("INFO", "[WG] Двусторонний трафик подтверждён, туннель активен", true)
				b.onEvent("connection_progress", map[string]any{
					"stage": "vpn", "state": "success", "message": "VPN-туннель активен",
				})
				b.onEvent("state_changed", "connected")
			}

		case core.EventError:
			b.recordLog("ERROR", ev.Message, false)
			b.onEvent("error", sanitizeDiagnosticText(ev.Message))

		case core.EventEvent:
			switch ev.Name {
			case "connection_progress":
				b.onEvent("connection_progress", ev.Data)
			case "diagnostic":
				b.onEvent("diagnostic_event", sanitizeDiagnosticText(ev.Data))
				b.recordLog("DEBUG", ev.Data, false)
			case "wg_config":
				b.onEvent("connection_progress", map[string]any{
					"stage": "vpn", "state": "running", "message": "Настройка VPN-маршрутов",
				})
				b.recordLog("INFO", "[WG] Применение конфига...", true)
				wgLogf := func(msg string) {
					b.recordLog("INFO", "[WG] "+msg, true)
				}
				if err := wg.Apply(ev.Data, ev.TurnIPs, wgLogf); err != nil {
					b.onEvent("connection_progress", map[string]any{
						"stage": "vpn", "state": "error", "message": "Не удалось настроить VPN",
					})
					b.onEvent("error", fmt.Sprintf("[WG] Ошибка: %v", err))
					b.mu.Lock()
					c := b.core
					b.mu.Unlock()
					if c != nil {
						go c.Stop()
					}
				} else {
					b.mu.Lock()
					b.wgApplied = true
					b.mu.Unlock()
					b.recordLog("INFO", "[WG] Конфиг применён, ожидаем подтверждения двустороннего трафика", true)
				}
			case "captcha_required":
				b.onEvent("captcha_required", ev.Data)
			case "ready":
				b.recordLog("INFO", "[ЯДРО] Рабочая DTLS-сессия установлена", true)
			default:
				b.onEvent("event", ev.Name)
			}
		}
	}

	// Канал событий закрывается и при штатном Stop, и при аварийном завершении ядра.
	// Teardown идемпотентен и гарантирует снятие временной IPv6-защиты.
	wg.Teardown()
	log.Printf("[BRIDGE] WG интерфейс и IPv6 leak protection сняты после завершения ядра")

	b.mu.Lock()
	b.running = false
	b.wgApplied = false
	b.connectedPublished = false
	b.core = nil
	var warning string
	if b.logFile != nil {
		if err := b.logFile.Close(); err != nil && !b.logWarningSent {
			warning = b.logFile.Warning()
			b.logWarningSent = true
		}
	}
	b.mu.Unlock()

	if warning != "" {
		b.onEvent("log", "WARN", warning)
	}
	b.onEvent("state_changed", "disconnected")
	log.Printf("[BRIDGE] Ядро завершилось")
}
