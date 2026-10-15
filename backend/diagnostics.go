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
	sensitiveDiagnosticPattern = regexp.MustCompile(`(?i)("?(?:access_token|anonymous_token|session_key|session_token|success_token|client_secret|credential|password|secret|device_id|user_id|token|hash|call_hash|hash_id|hashid|deviceid|userid|sessionid|anonym_name|anonymname|anonymous_name|anonymousname|username|user_name|hostname|first_name|last_name|display_name|full_name|profile_name|profilename|server|link)"?\s*[:=]\s*"?)([^"\s,}\]&]+)`)
	quotedDiagnosticPattern = regexp.MustCompile(`(?i)("?(?:access_token|anonymous_token|session_key|session_token|success_token|client_secret|credential|password|secret|device_id|user_id|token|hash|call_hash|hash_id|hashid|deviceid|userid|sessionid|anonym_name|anonymname|anonymous_name|anonymousname|username|user_name|hostname|first_name|last_name|display_name|full_name|profile_name|profilename|server|link)"?\s*[:=]\s*)"[^"]*"`)
	diagnosticURLPattern = regexp.MustCompile(`(?i)(?:\b(?:https?|wss?|vkcalls?|wdtt|tg)://|\bvk\.com/call/(?:join/)?)[^\s"'<>\x60]+`)
	diagnosticEncodedURLPattern = regexp.MustCompile(`(?i)(?:https?|wss?|vkcalls?|wdtt|tg)%3a%2f%2f[^\s"'<>\x60]+`)
	diagnosticDoubleEncodedURLPattern = regexp.MustCompile(`(?i)(?:https?|wss?)%253a%252f%252f[^\s"'<>\x60]+`)
	diagnosticEncodedFieldPattern = regexp.MustCompile(`(?i)((?:%22)?(?:anonymname|anonymous_name|user_id|device_id|access_token|password|hash|hash_id|call_hash|hashid|deviceid|userid|anonym_name|profilename|session_token|username|profile_name|link)(?:%22)?%(?:3a|3d))[^&\s"'<>]+`)
	diagnosticQueryPattern = regexp.MustCompile(`(?i)([?&][a-z_][a-z0-9_.-]*=)[^&\s"'<>]+`)
	windowsUserPathPattern = regexp.MustCompile(`(?i)(?:[a-z]:(?:\\|/)|\\\\)[^"'<>,;|\r\n]+`)
	unixUserPathPattern = regexp.MustCompile(`(?:/home/|/Users/|/root/|/tmp/|/var/folders/)[^"'<>,;|\s]+`)
)

func sanitizeDiagnosticText(message string) string {
	message = diagnosticEncodedURLPattern.ReplaceAllString(message, "[link redacted]")
	message = diagnosticDoubleEncodedURLPattern.ReplaceAllString(message, "[link redacted]")
	message = diagnosticURLPattern.ReplaceAllString(message, "[link redacted]")
	message = diagnosticEncodedFieldPattern.ReplaceAllString(message, "${1}[redacted]")
	message = windowsUserPathPattern.ReplaceAllString(message, "[path redacted]")
	message = unixUserPathPattern.ReplaceAllString(message, "[path redacted]")
	message = quotedDiagnosticPattern.ReplaceAllString(message, `${1}"[redacted]"`)
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
	event.Server = "[redacted]" // user-defined profile names are not diagnostic identifiers
	event.HashID = "[redacted]"
	event.Message = sanitizeDiagnosticText(event.Message)
	event.OperationID = sanitizeDiagnosticText(event.OperationID)
	event.Stage = sanitizeDiagnosticText(event.Stage)
	event.Action = sanitizeDiagnosticText(event.Action)
	event.Result = sanitizeDiagnosticText(event.Result)
	a.onBridgeEvent("diagnostic_event", event)
}
