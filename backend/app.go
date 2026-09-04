package backend

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

const Version = "1.7.0"

// App — главный объект приложения.
// Wails привязывает его методы к frontend через Bind().
type App struct {
	ctx      context.Context
	bridge   *Bridge
	store    *Store
	vkClient *vkAPIClient
	vkMu     sync.Mutex
	vkCancel context.CancelFunc
}

// NewApp создаёт App. Вызывается из main() до wails.Run().
func NewApp() *App {
	return &App{
		store:    NewStore(),
		vkClient: newVKAPIClient(),
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
	hashes, err := a.prepareVKHashes(params)
	if err != nil {
		return err
	}
	params.Hashes = hashes
	return a.bridge.Connect(params)
}

func (a *App) Disconnect() {
	a.bridge.Disconnect()
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

func (a *App) CheckUpdate() *UpdateInfo {
	info, err := CheckUpdate(Version)
	if err != nil {
		return &UpdateInfo{Available: false}
	}
	return info
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

func (a *App) beginVKOperation() (context.Context, func(), error) {
	a.vkMu.Lock()
	defer a.vkMu.Unlock()
	if a.vkCancel != nil {
		return nil, nil, errors.New("операция VK уже выполняется")
	}
	parent := a.ctx
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
