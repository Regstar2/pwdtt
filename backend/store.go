package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Store — пассивный модуль хранения данных.
// Все записи атомарные (write to tmp → rename).
// Не инициирует ничего — только отвечает на запросы.
//
// Структура файлов:
//
//	~/.config/pwdtt/config.json        — AppSettings
//	~/.config/pwdtt/servers/<name>.json — ProfileData (серверы)
//	~/.config/pwdtt/logs/*.log          — логи сессий
type Store struct {
	baseDir string // ~/.config/pwdtt
	mu sync.Mutex // serializes settings and profile file operations
	recoveryNotices []string
}

// NewStore создаёт Store и создаёт директории если нужно.
func NewStore() *Store {
	dir := configDir()
	os.MkdirAll(filepath.Join(dir, "servers"), 0o755)
	os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	return &Store{baseDir: dir}
}

// ═══════════════════════════════════════════════════
// SETTINGS — настройки приложения
// ═══════════════════════════════════════════════════

// LoadSettings использует defaults только для отсутствующего или сохранённого
// в карантин повреждённого config.json. Ошибки чтения также показываются в UI.
func (s *Store) LoadSettings() AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	defaults := AppSettings{AutoStart: true, ObfsMode: "audio", ObfsAccepted: false, DebugLogging: false}
	path := filepath.Join(s.baseDir, "config.json")
	data, err := s.readJSON(path, "файл настроек", validateSettingsJSON)
	if err != nil {
		return defaults
	}
	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return defaults // validated above
	}
	if !settings.ObfsAccepted {
		settings.ObfsMode = "audio"
	}
	return settings
}

// SaveSettings не перезаписывает нечитаемый или некопируемый config.json.
func (s *Store) SaveSettings(settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.baseDir, "config.json")
	if _, err := s.readJSON(path, "файл настроек", validateSettingsJSON); err != nil &&
		!errors.Is(err, os.ErrNotExist) && !errors.Is(err, errQuarantinedJSON) {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil { return err }
	return atomicWrite(path, data)
}

// ═══════════════════════════════════════════════════
// PROFILES — CRUD для серверов
// ═══════════════════════════════════════════════════

// sanitizeProfileName оставляет Unicode-буквы/цифры и безопасные разделители имени файла.
func sanitizeProfileName(name string) string {
	var out []rune
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ' ' {
			out = append(out, r)
		}
	}
	return strings.TrimSpace(string(out))
}

// LoadProfile возвращает ошибку для повреждённого профиля после карантина.
func (s *Store) LoadProfile(name string) (*ProfileData, error) {
	name = sanitizeProfileName(name)
	if name == "" { return nil, fmt.Errorf("invalid profile name") }
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadProfileLocked(name)
}

func (s *Store) loadProfileLocked(name string) (*ProfileData, error) {
	path := filepath.Join(s.baseDir, "servers", name+".json")
	data, err := s.readJSON(path, "профиль сервера", validateProfileJSON)
	if err != nil { return nil, fmt.Errorf("profile: %w", err) }
	var p ProfileData
	if err := json.Unmarshal(data, &p); err != nil { return nil, err }
	return &p, nil
}

// SaveProfile сохраняет профиль атомарно и сохраняет повреждённую версию
// в карантин до записи новых данных.
func (s *Store) SaveProfile(name string, p ProfileData) error {
	name = sanitizeProfileName(name)
	if name == "" { return fmt.Errorf("invalid profile name") }
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.baseDir, "servers", name+".json")
	data, err := s.readJSON(path, "профиль сервера", validateProfileJSON)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errQuarantinedJSON) {
		return err
	}
	if err == nil {
		var existing ProfileData
		if err := json.Unmarshal(data, &existing); err != nil { return err }
		if p.DeviceID == "" { p.DeviceID = existing.DeviceID }
		if p.HashPolicy == nil { p.HashPolicy = existing.HashPolicy }
	}
	if p.DeviceID == "" { p.DeviceID = uuid.New().String() }
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil { return err }
	return atomicWrite(path, encoded)
}

// DeleteProfile удаляет профиль сервера.
func (s *Store) DeleteProfile(name string) error {
	name = sanitizeProfileName(name)
	if name == "" { return fmt.Errorf("invalid profile name") }
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(filepath.Join(s.baseDir, "servers", name+".json"))
}

// ListProfiles сохраняет повреждённые JSON отдельно и регистрирует уведомление,
// вместо молчаливого пропуска. Резервные копии в список не попадают.
func (s *Store) ListProfiles() map[string]ProfileData {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.baseDir, "servers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.addRecoveryNotice("Не удалось прочитать каталог профилей серверов.")
		return nil
	}
	result := make(map[string]ProfileData)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") { continue }
		data, err := s.readJSON(filepath.Join(dir, entry.Name()), "профиль сервера", validateProfileJSON)
		if err != nil { continue }
		var profile ProfileData
		if err := json.Unmarshal(data, &profile); err != nil { continue }
		result[strings.TrimSuffix(entry.Name(), ".json")] = profile
	}
	return result
}

// CreateLogFile создаёт файл лога для сессии.
func (s *Store) CreateLogFile(peerIP string) (*os.File, error) {
	dir := filepath.Join(s.baseDir, "logs")
	return os.CreateTemp(dir, "*.log")
}

// errQuarantinedJSON означает, что оригинал уже сохранён в .corrupt-*.bak.
// Не возвращаем абсолютные пути или имя профиля в пользовательские уведомления.
var errQuarantinedJSON = errors.New("corrupt JSON was quarantined")

type jsonValidator func([]byte) error

func validateJSONObject(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if !strings.HasPrefix(trimmed, "{") || !json.Valid(data) {
		return errors.New("expected a JSON object")
	}
	return nil
}

func validateSettingsJSON(data []byte) error {
	if err := validateJSONObject(data); err != nil { return err }
	var settings AppSettings
	return json.Unmarshal(data, &settings)
}

func validateProfileJSON(data []byte) error {
	if err := validateJSONObject(data); err != nil { return err }
	var profile ProfileData
	return json.Unmarshal(data, &profile)
}

func (s *Store) addRecoveryNotice(message string) {
	for _, existing := range s.recoveryNotices {
		if existing == message { return }
	}
	s.recoveryNotices = append(s.recoveryNotices, message)
}

// RecoveryNotices позволяет UI показать результаты восстановления, даже если
// событие возникло до подписки на Wails events.
func (s *Store) RecoveryNotices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.recoveryNotices...)
}

func (s *Store) readJSON(path, kind string, validate jsonValidator) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.addRecoveryNotice("Не удалось прочитать "+kind+". Файл не изменён.")
		}
		return nil, err
	}
	if err := validate(data); err == nil { return data, nil }

	// Переименование в том же каталоге не затрагивает исходные байты.
	backup := path + ".corrupt-" + time.Now().UTC().Format("20060102T150405") + "-" + uuid.NewString() + ".bak"
	if err := os.Rename(path, backup); err != nil {
		s.addRecoveryNotice("Повреждённый "+kind+" не удалось сохранить. Запись заблокирована.")
		return nil, fmt.Errorf("quarantine %s failed: %w", kind, err)
	}
	if err := os.Chmod(backup, 0o600); err != nil {
		s.addRecoveryNotice("Копия повреждённого JSON создана, но не удалось ограничить права на неё.")
	}
	s.addRecoveryNotice("Повреждённый "+kind+" сохранён в резервной копии (.corrupt-*.bak). Проверьте настройки.")
	return nil, errQuarantinedJSON
}

// ═══════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════

// configDir возвращает ~/.config/pwdtt
func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	return filepath.Join(base, "pwdtt")
}

// atomicWrite использует закрытый временный файл и rename в том же каталоге.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil { return err }
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil { _ = f.Close(); return err }
	if _, err := f.Write(data); err != nil { _ = f.Close(); return err }
	if err := f.Sync(); err != nil { _ = f.Close(); return err }
	if err := f.Close(); err != nil { return err }
	return os.Rename(f.Name(), path)
}

// NewTestStore создаёт Store с временной директорией (для тестов).
// Изолирует HOME/XDG_CONFIG_HOME и создаёт нужные поддиректории.
func NewTestStore(t interface{ TempDir() string; Setenv(string, string) }) *Store {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, ".config"))
	os.MkdirAll(filepath.Join(dir, ".config", "pwdtt", "servers"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".config", "pwdtt", "logs"), 0o755)
	return NewStore()
}
