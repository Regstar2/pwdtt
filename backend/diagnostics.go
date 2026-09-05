package backend

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type diagnosticEvent struct {
	Timestamp   int64  `json:"timestamp"`
	Level       string `json:"level"`
	Subsystem   string `json:"subsystem"`
	OperationID string `json:"operationId,omitempty"`
	HashID      string `json:"hashId,omitempty"`
	Server      string `json:"server,omitempty"`
	Stage       string `json:"stage,omitempty"`
	Action      string `json:"action,omitempty"`
	Result      string `json:"result,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	ElapsedMs   int64  `json:"elapsedMs,omitempty"`
	DurationMs  int64  `json:"durationMs,omitempty"`
	Message     string `json:"message,omitempty"`
}

func newOperationID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "op"
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(id) > 10 {
		id = id[:10]
	}
	return prefix + "-" + id
}

func (a *App) GetDebugLogging() bool {
	return a.store.LoadSettings().DebugLogging
}

func (a *App) SetDebugLogging(enabled bool) error {
	settings := a.store.LoadSettings()
	settings.DebugLogging = enabled
	if err := a.store.SaveSettings(settings); err != nil {
		return err
	}
	if a.bridge != nil {
		a.bridge.SetDebugLogging(enabled)
	}
	a.onBridgeEvent("diagnostic-mode-changed", enabled)
	return nil
}

func (a *App) emitDiagnostic(event diagnosticEvent) {
	if a == nil || a.store == nil || !a.store.LoadSettings().DebugLogging {
		return
	}
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}
	if strings.TrimSpace(event.Level) == "" {
		event.Level = "DEBUG"
	}
	a.onBridgeEvent("diagnostic_event", event)
}
