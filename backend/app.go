package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Version is injected from the release tag with Go ldflags.\n// Local/development builds intentionally keep the explicit fallback.\nvar Version = "dev"

// App — главный объект приложения.
// Wails привязывает его методы к frontend через Bind().
type App struct {
	ctx      context.Context
	bridge   *Bridge
	store    *Store
	vkClient *vkAPIClient
	vkMu     sync.Mutex
	vkCancel context.CancelFunc
	hashOps       *vkHashOperationState
	connectMu     sync.Mutex
	connectCancel context.CancelFunc
}

// NewApp создаёт App. Вызывается из main() до wails.Run().
func NewApp() *App {
	return &App{
		store:    NewStore(),
		vkClient: newVKAPIClient(),
		hashOps:  newVKHashOperationState(),
	}
}

// ═══════════════════════════════════════════════════
// WAILS LIFECYCLE
// ═══════════════════════════════════════════════════

// Startup вызывается Wails после создания webview.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	settings := a.store.LoadSettings()
	a.bridge = NewBridge(ctx, a.store, a.onBridgeEvent)

	if settings.AutoStart {
		a.SetAutoStart(true)
	}

	// Уборка маршрутов, оставшихся от краша прошлого запуска
	go CleanupStaleExcludeRoutes(func(msg string) {
		a.onBridgeEvent("log", "INFO", "[WG] "+msg)
	})
}

// Shutdown освобождает внешнее сетевое состояние перед завершением приложения.
func (a *App) Shutdown(ctx context.Context) {
	wg.Teardown()
}

// ═══════════════════════════════════════════════════
// WAILS BINDINGS
// ═══════════════════════════════════════════════════

func (a *App) Connect(params ConnectParams) error {
	if a.hashOps == nil {
		a.hashOps = newVKHashOperationState()
	}
	a.hashOps.cancelInteractive()

	a.connectMu.Lock()
	if a.connectCancel != nil {
		a.connectMu.Unlock()
		return errors.New("подключение уже выполняется")
	}
	connectCtx, cancelConnect := context.WithCancel(a.appContext())
	a.connectCancel = cancelConnect
	a.connectMu.Unlock()
	defer func() {
		cancelConnect()
		a.connectMu.Lock()
		a.connectCancel = nil
		a.connectMu.Unlock()
	}()

	operationID := newOperationID("connect")
	started := time.Now()
	a.emitDiagnostic(diagnosticEvent{
		Subsystem: "CONNECT", OperationID: operationID, Stage: "preflight",
		Action: "start", Server: params.ProfileName,
	})

	hashes, err := a.prepareVKHashes(connectCtx, params, operationID)
	if err != nil {
		a.emitDiagnostic(diagnosticEvent{
			Level: "ERROR", Subsystem: "CONNECT", OperationID: operationID,
			Stage: "preflight", Action: "complete", Result: "error",
			Server: params.ProfileName, DurationMs: time.Since(started).Milliseconds(),
			Message: err.Error(),
		})
		return err
	}
	if err := connectCtx.Err(); err != nil {
		return fmt.Errorf("подключение отменено: %w", err)
	}
	params.Hashes = hashes
	params.OperationID = operationID

	a.emitDiagnostic(diagnosticEvent{
		Subsystem: "CONNECT", OperationID: operationID, Stage: "preflight",
		Action: "complete", Result: "ok", Server: params.ProfileName,
		DurationMs: time.Since(started).Milliseconds(),
		Message: "VK hash preflight completed",
	})
	return a.bridge.Connect(params)
}

func (a *App) Disconnect() {
	a.connectMu.Lock()
	cancel := a.connectCancel
	a.connectMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if a.bridge != nil {
		a.bridge.Disconnect()
	}
}

func (a *App) IsRunning() bool {
	return a.bridge.IsRunning()
}

func (a *App) GetVersion() string {
	return Version
}

func latencyHost(peerAddr string) string {
	value := strings.TrimSpace(peerAddr)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]")
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return strings.Trim(value, "[]")
	}
	return value
}

func latencyPingArgs(host string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"-n", "1", "-w", "1200", host}
	case "darwin":
		return []string{"-c", "1", "-W", "1200", host}
	default:
		return []string{"-c", "1", "-W", "1", host}
	}
}

// MeasureLatency измеряет ICMP-задержку до peer-адреса сервера.
// Возвращает -1, если сервер не ответил или ping недоступен.
func (a *App) MeasureLatency(peerAddr string) int {
	host := latencyHost(peerAddr)
	if host == "" {
		return -1
	}

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, "ping", latencyPingArgs(host)...)
	configureBackgroundCommand(cmd)
	if err := cmd.Run(); err != nil {
		return -1
	}

	ms := time.Since(start).Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return int(ms)
}

// ═══════════════════════════════════════════════════
// PROFILES
// ═══════════════════════════════════════════════════

func (a *App) GetProfile(name string) (*ProfileData, error) {
	return a.store.LoadProfile(name)
}

func (a *App) SaveProfile(name string, p ProfileData) error {
	return a.store.SaveProfile(name, p)
}

func (a *App) DeleteProfile(name string) error {
	return a.store.DeleteProfile(name)
}

func (a *App) ListProfiles() map[string]ProfileData {
	return a.store.ListProfiles()
}

// ═══════════════════════════════════════════════════
// SETTINGS
// ═══════════════════════════════════════════════════

func (a *App) GetAutoStart() bool {
	return a.store.LoadSettings().AutoStart
}

func (a *App) SetAutoStart(v bool) error {
	settings := a.store.LoadSettings()
	settings.AutoStart = v
	a.store.SaveSettings(settings)
	return setAutoStart(v)
}

func (a *App) GetObfsMode() string {
	return a.store.LoadSettings().ObfsMode
}

func (a *App) SetObfsMode(mode string) error {
	settings := a.store.LoadSettings()
	settings.ObfsMode = mode
	return a.store.SaveSettings(settings)
}

func (a *App) GetObfsAccepted() bool {
	return a.store.LoadSettings().ObfsAccepted
}

func (a *App) SetObfsAccepted(v bool) error {
	settings := a.store.LoadSettings()
	settings.ObfsAccepted = v
	return a.store.SaveSettings(settings)
}

func (a *App) CheckUpdate() (*UpdateInfo, error) {
	if !isComparableVersion(Version) {
		return &UpdateInfo{Available: false}, nil
	}
	return CheckUpdate(Version)
}

func (a *App) CancelVKOperation() {
	a.vkMu.Lock()
	defer a.vkMu.Unlock()
	if a.vkCancel != nil {
		a.vkCancel()
	}
}

// ═══════════════════════════════════════════════════
// INTERNAL
// ═══════════════════════════════════════════════════

func (a *App) appContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) beginVKOperation() (context.Context, func(), error) {
	return a.beginVKOperationWithParent(a.appContext())
}

func (a *App) beginVKOperationWithParent(parent context.Context) (context.Context, func(), error) {
	a.vkMu.Lock()
	defer a.vkMu.Unlock()
	if a.vkCancel != nil {
		return nil, nil, errors.New("операция VK уже выполняется")
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.vkCancel = cancel
	return ctx, func() {
		cancel()
		a.vkMu.Lock()
		a.vkCancel = nil
		a.vkMu.Unlock()
	}, nil
}

func (a *App) onBridgeEvent(name string, args ...any) {
	wails.EventsEmit(a.ctx, name, args...)
}
