package backend

import (
	"regexp"
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

var (
	sensitiveDiagnosticPattern = regexp.MustCompile(`(?i)("?(?:access_token|anonymous_token|session_key|session_token|success_token|client_secret|credential|password|secret|device_id|user_id|token|hash|call_hash|username|user_name|hostname|first_name|last_name|display_name|full_name|profile_name)"?\\s*[:=]\\s*"?)([^"\\s,}\\]&]+)`)
	quotedDiagnosticPattern = regexp.MustCompile(`(?i)("?(?:access_token|anonymous_token|session_key|session_token|success_token|client_secret|credential|password|secret|device_id|user_id|token|hash|call_hash|username|user_name|hostname|first_name|last_name|display_name|full_name|profile_name)"?\\s*[:=]\\s*)"[^"]*"`)
	diagnosticURLPattern = regexp.MustCompile(`(?i)(?:\\b(?:https?|wss?|vkcalls?|wdtt|tg)://|\\bvk\\.com/call/(?:join/)?)[^\\s"'<>\\x60]+`)
	diagnosticQueryPattern = regexp.MustCompile(`(?i)([?&][a-z_][a-z0-9_.-]*=)[^&\\s"'<>]+`)
	windowsUserPathPattern = regexp.MustCompile(`(?i)(?:[a-z]:\\\\|\\\\\\\\)[^"'<>,;|\\r\\n]+`)
	unixUserPathPattern = regexp.MustCompile(`(?:/home/|/Users/|/root/|/tmp/|/var/folders/)[^"'<>,;|\\s]+`)
)

func sanitizeDiagnosticText(message string) string {
	message = diagnosticURLPattern.ReplaceAllString(message, "[link redacted]")
	message = windowsUserPathPattern.ReplaceAllString(message, "[path redacted]")
	message = unixUserPathPattern.ReplaceAllString(message, "[path redacted]")
	message = quotedDiagnosticPattern.ReplaceAllString(message, "${1}\\\"[redacted]\\\"")
	message = sensitiveDiagnosticPattern.ReplaceAllString(message, "${1}[redacted]")
	return diagnosticQueryPattern.ReplaceAllString(message, "${1}[redacted]")
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
