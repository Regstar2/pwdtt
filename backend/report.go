package backend

import (
	"fmt"
	"runtime"
	"strings"
)

type LogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Count   int    `json:"count"`
}

// GenerateReport exports a consistent snapshot of the active or last session.
// UI entries are the fallback for preflight events and unavailable file logs.
func (a *App) GenerateReport(entries []LogEntry) string {
	var sb strings.Builder
	sb.WriteString("=== PWDTT Bug Report ===\n\n")
	sb.WriteString("## Device\n")
	fmt.Fprintf(&sb, "- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&sb, "- Go: %s\n", runtime.Version())
	fmt.Fprintf(&sb, "- App Version: %s\n", Version)
	if privilege := platformPrivilegeReport(); privilege != "" {
		fmt.Fprintf(&sb, "- %s\n", sanitizeDiagnosticText(privilege))
	}
	sb.WriteByte('\n')

	// Cap caller-supplied UI history as well as the on-disk journal.
	if len(entries) > 300 {
		entries = entries[len(entries)-300:]
	}
	var ui strings.Builder
	for _, entry := range entries {
		level := strings.ToUpper(strings.TrimSpace(entry.Level))
		if level == "" {
			level = "INFO"
		}
		fmt.Fprintf(&ui, "[%s] [%s] %s", sanitizeDiagnosticText(entry.Time),
			sanitizeDiagnosticText(level), boundedReportText(entry.Message))
		if entry.Count > 1 {
			fmt.Fprintf(&ui, " (x%d)", entry.Count)
		}
		ui.WriteByte('\n')
	}
	if ui.Len() > 0 {
		sb.WriteString("## UI and structured diagnostics\n```\n")
		sb.WriteString(ui.String())
		sb.WriteString("```\n\n")
	}

	sb.WriteString("## Session log file\n")
	var snapshot sessionLogSnapshot
	var exists bool
	if a != nil && a.bridge != nil {
		snapshot, exists = a.bridge.SnapshotSessionLog()
	}
	if !exists {
		sb.WriteString("Warning: session journal unavailable.\n")
	}
	for _, warning := range snapshot.warnings {
		fmt.Fprintf(&sb, "Warning: %s\n", boundedReportText(warning))
	}
	content := sanitizeDiagnosticText(snapshot.content)
	content = strings.ReplaceAll(content, "```", "` ` `")
	if content == "" && ui.Len() > 0 {
		content = ui.String()
		sb.WriteString("Warning: using bounded UI history as session-log fallback.\n")
	} else if snapshot.fallback {
		sb.WriteString("Warning: using bounded in-memory session history.\n")
	}
	if content == "" {
		sb.WriteString("(no session events)\n")
	} else {
		sb.WriteString("```\n")
		sb.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			sb.WriteByte('\n')
		}
		sb.WriteString("```\n")
	}
	return sb.String()
}

func boundedReportText(message string) string {
	message = sanitizeDiagnosticText(message)
	message = strings.NewReplacer("\r", "\\r", "\n", "\\n", "```", "` ` `").Replace(message)
	if len(message) <= sessionLogLineLimit {
		return message
	}
	for i := range message {
		if i >= sessionLogLineLimit-16 {
			return message[:i] + " [truncated]"
		}
	}
	return message
}
