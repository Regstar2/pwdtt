package backend

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sessionLogFileLimit   = 512 * 1024
	sessionLogMemoryLimit = 128 * 1024
	sessionLogLineLimit   = 4096
	sessionLogRetention   = 10
)

// LogFile owns a single connection's journal. All I/O and snapshots are serialized.
type LogFile struct {
	mu        sync.Mutex
	file      *os.File
	path      string
	size      int64
	lines     int
	ring      []string
	ringBytes int
	warnings  []string
	closed    bool
	truncated bool
	diskFault bool
}

func (lf *LogFile) warnLocked(message string) {
	for _, warning := range lf.warnings {
		if warning == message {
			return
		}
	}
	lf.warnings = append(lf.warnings, message)
}

// OpenLogFile always returns a session journal, including when the filesystem
// is unavailable. The bounded in-memory history remains usable on I/O failure.
func (s *Store) OpenLogFile() *LogFile {
	lf := &LogFile{}
	dir := filepath.Join(s.baseDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		lf.warnLocked("Не удалось создать каталог журнала сессии.")
		return lf
	}
	f, err := os.CreateTemp(dir, "session-*.log")
	if err != nil {
		lf.warnLocked("Не удалось создать файл журнала сессии.")
		return lf
	}
	lf.file, lf.path = f, f.Name()
	if err := pruneSessionLogs(dir, filepath.Base(f.Name())); err != nil {
		lf.warnLocked("Не удалось очистить старые журналы сессий.")
	}
	return lf
}

type retainedLog struct {
	name string
	time time.Time
}

func pruneSessionLogs(dir, active string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var logs []retainedLog
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".log") || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		logs = append(logs, retainedLog{name: entry.Name(), time: info.ModTime()})
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].time.Equal(logs[j].time) {
			return logs[i].name < logs[j].name
		}
		return logs[i].time.Before(logs[j].time)
	})
	var firstErr error
	excess := len(logs) - sessionLogRetention
	for _, oldest := range logs {
		if excess <= 0 {
			break
		}
		if oldest.name == active {
			continue
		}
		if err := os.Remove(filepath.Join(dir, oldest.name)); err != nil {
			firstErr = errors.Join(firstErr, err)
		} else {
			excess--
		}
	}
	return firstErr
}

func boundedLogLine(level, message string) string {
	level = strings.ToUpper(strings.TrimSpace(level))
	if level == "" {
		level = "INFO"
	}
	message = sanitizeDiagnosticText(message)
	message = strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(message)
	line := fmt.Sprintf("[%s] [%s] %s", time.Now().Format("15:04:05"), level, message)
	if len(line) > sessionLogLineLimit-16 {
		size := 0
		for i, r := range line {
			if size+len(string(r)) > sessionLogLineLimit-16 {
				line = line[:i] + " [truncated]"
				break
			}
			size += len(string(r))
		}
	}
	return line + "\n"
}

func (lf *LogFile) Write(level, message string) error {
	if lf == nil {
		return nil
	}
	line := boundedLogLine(level, message)
	lf.mu.Lock()
	defer lf.mu.Unlock()
	lf.ring = append(lf.ring, line)
	lf.ringBytes += len(line)
	for lf.ringBytes > sessionLogMemoryLimit {
		lf.ringBytes -= len(lf.ring[0])
		lf.ring = lf.ring[1:]
	}
	if lf.file == nil {
		return errors.New("session log file unavailable")
	}
	if lf.closed {
		lf.warnLocked("Попытка записи в закрытый журнал сессии.")
		return errors.New("session log file closed")
	}
	if lf.truncated {
		return nil
	}
	if lf.size+int64(len(line)) > sessionLogFileLimit {
		lf.truncated = true
		lf.warnLocked("Файл журнала достиг лимита 512 КиБ; последние события сохранены в памяти.")
		return nil
	}
	n, err := lf.file.WriteString(line)
	if err != nil || n != len(line) {
		lf.warnLocked("Не удалось записать событие в файл журнала сессии.")
		lf.diskFault = true
		if err == nil {
			err = fmt.Errorf("short write: %d/%d", n, len(line))
		}
		return err
	}
	lf.size += int64(n)
	lf.lines++
	if lf.lines%50 == 0 {
		if err := lf.file.Sync(); err != nil {
			lf.warnLocked("Не удалось синхронизировать файл журнала сессии.")
			lf.diskFault = true
			return err
		}
	}
	return nil
}

func (lf *LogFile) Close() error {
	if lf == nil {
		return nil
	}
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if lf.closed {
		return nil
	}
	lf.closed = true
	if lf.file == nil {
		return nil
	}
	syncErr := lf.file.Sync()
	closeErr := lf.file.Close()
	if syncErr != nil {
		lf.warnLocked("Не удалось синхронизировать файл журнала при завершении сессии.")
		lf.diskFault = true
	}
	if closeErr != nil {
		lf.warnLocked("Не удалось закрыть файл журнала сессии.")
		lf.diskFault = true
	}
	return errors.Join(syncErr, closeErr)
}

type sessionLogSnapshot struct {
	content  string
	warnings []string
	fallback bool
}

func (lf *LogFile) Snapshot() sessionLogSnapshot {
	if lf == nil {
		return sessionLogSnapshot{}
	}
	lf.mu.Lock()
	defer lf.mu.Unlock()

	if lf.file != nil && !lf.closed {
		if err := lf.file.Sync(); err != nil {
			lf.warnLocked("Не удалось синхронизировать файл журнала при экспорте.")
			lf.diskFault = true
		}
	}
	snapshot := sessionLogSnapshot{warnings: append([]string(nil), lf.warnings...)}
	if lf.file != nil && !lf.truncated && !lf.diskFault {
		data, err := os.ReadFile(lf.path)
		if err == nil && len(data) > 0 {
			snapshot.content = sanitizeDiagnosticText(string(data))
			snapshot.warnings = append([]string(nil), lf.warnings...)
			return snapshot
		}
		if err != nil {
			lf.warnLocked("Не удалось прочитать файл журнала сессии.")
		}
	}
	snapshot.content = strings.Join(lf.ring, "")
	snapshot.fallback = len(snapshot.content) != 0
	snapshot.warnings = append([]string(nil), lf.warnings...)
	if snapshot.fallback && lf.file != nil && !lf.truncated && len(snapshot.warnings) == 0 {
		snapshot.warnings = append(snapshot.warnings, "Файл журнала пуст; использована история событий в памяти.")
	}
	return snapshot
}

func (lf *LogFile) Warning() string {
	if lf == nil {
		return ""
	}
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return strings.Join(lf.warnings, " ")
}

// ReadLatestLogWithError is kept for legacy callers; reports use the specific
// session journal instead of selecting an unrelated file by filename.
func (s *Store) ReadLatestLogWithError() (string, error) {
	dir := filepath.Join(s.baseDir, "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var latest string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".log") && entry.Name() > latest {
			latest = entry.Name()
		}
	}
	if latest == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(dir, latest))
	if err != nil {
		return "", err
	}
	return sanitizeDiagnosticText(string(data)), nil
}

func (s *Store) ReadLatestLog() string {
	content, err := s.ReadLatestLogWithError()
	if err != nil {
		log.Print("[SESSION LOG] Ошибка чтения последнего журнала.")
	}
	return content
}
