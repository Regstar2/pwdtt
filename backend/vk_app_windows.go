//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const vkOAuthTimeout = 5 * time.Minute

type vkSession struct {
	LoggedIn   bool      `json:"logged_in,omitempty"`
	AccessToken string    `json:"access_token,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

func (a *App) IsVKAuthAvailable() bool {
	return true
}

func (a *App) IsVKLoggedIn() bool {
	session, err := loadVKSession()
	if err != nil {
		return false
	}
	if session.LoggedIn {
		return true
	}
	if session.AccessToken == "" {
		return false
	}
	return session.ExpiresAt.IsZero() || session.ExpiresAt.After(time.Now())
}

func (a *App) VKLogin() error {
	ctx, done, err := a.beginVKOperation()
	if err != nil {
		return err
	}
	defer done()

	loginCtx, cancel := context.WithTimeout(ctx, vkOAuthTimeout)
	defer cancel()

	if _, err := runLegacyVKAuthHelper(loginCtx, "login"); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(loginCtx.Err(), context.DeadlineExceeded) {
			return errors.New("время ожидания авторизации VK истекло")
		}
		if errors.Is(err, context.Canceled) {
			return errors.New("операция VK отменена")
		}
		return err
	}

	session, _ := loadVKSession()
	session.LoggedIn = true
	if err := saveVKSession(session); err != nil {
		return fmt.Errorf("не удалось сохранить состояние входа VK: %w", err)
	}
	a.onBridgeEvent("vk-auth-changed", true)
	return nil
}

func (a *App) VKLogout() error {
	ctx, done, err := a.beginVKOperation()
	if err != nil {
		return err
	}
	defer done()

	if err := deleteVKSession(); err != nil {
		return fmt.Errorf("не удалось удалить локальную авторизацию VK: %w", err)
	}
	a.onBridgeEvent("vk-auth-changed", false)

	clearCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := runLegacyVKAuthHelper(clearCtx, "clear"); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(clearCtx.Err(), context.DeadlineExceeded) {
			return errors.New("локальное состояние удалено, но не удалось очистить cookies VK")
		}
		return fmt.Errorf("локальное состояние удалено, но cookies VK не очищены: %w", err)
	}
	return nil
}

func (a *App) GenerateVKHashes(count int, existing []string) ([]string, error) {
	ctx, done, err := a.beginVKOperation()
	if err != nil {
		return nil, err
	}
	defer done()

	accessToken, err := a.ensureVKAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	hashes, err := generateVKHashes(ctx, a.vkClient, accessToken, count, existing, func(hash string, current, total int) {
		a.onBridgeEvent("vk-hash-generated", hash, current, total)
	})
	if err != nil {
		var apiErr *vkAPIError
		if errors.As(err, &apiErr) && apiErr.Code == 5 {
			_ = clearSavedVKAccessToken()
			return hashes, errors.New("VK API отклонил access token; повторите создание хеша, чтобы получить новый токен")
		}
		if errors.Is(err, context.Canceled) {
			return hashes, errors.New("операция VK отменена")
		}
		return hashes, err
	}
	return hashes, nil
}

func (a *App) ensureVKAccessToken(ctx context.Context) (string, error) {
	session, err := loadVKSession()
	if err != nil || (!session.LoggedIn && session.AccessToken == "") {
		return "", errors.New("сначала войдите в VK")
	}
	if session.AccessToken != "" && (session.ExpiresAt.IsZero() || session.ExpiresAt.After(time.Now().Add(30*time.Second))) {
		return session.AccessToken, nil
	}

	tokenCtx, cancel := context.WithTimeout(ctx, vkOAuthTimeout)
	defer cancel()
	token, err := runLegacyVKAuthHelper(tokenCtx, "token")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(tokenCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("время ожидания токена VK истекло")
		}
		if errors.Is(err, context.Canceled) {
			return "", errors.New("операция VK отменена")
		}
		return "", fmt.Errorf("не удалось получить токен VK: %w", err)
	}
	if token.AccessToken == "" {
		return "", errors.New("VK OAuth не вернул access token")
	}

	session.LoggedIn = true
	session.AccessToken = token.AccessToken
	session.ExpiresAt = time.Time{}
	if token.ExpiresIn > 0 {
		session.ExpiresAt = time.Now().Add(token.ExpiresIn)
	}
	if err := saveVKSession(session); err != nil {
		return "", fmt.Errorf("не удалось сохранить токен VK: %w", err)
	}
	return session.AccessToken, nil
}

func clearSavedVKAccessToken() error {
	session, err := loadVKSession()
	if err != nil {
		return err
	}
	session.AccessToken = ""
	session.ExpiresAt = time.Time{}
	return saveVKSession(session)
}

func vkSessionFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "PWDTT", "vk_session.bin"), nil
}

func saveVKSession(session vkSession) error {
	plain, err := json.Marshal(session)
	if err != nil {
		return err
	}
	protected, err := protectWindowsData(plain)
	if err != nil {
		return err
	}
	path, err := vkSessionFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, protected, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func loadVKSession() (vkSession, error) {
	path, err := vkSessionFilePath()
	if err != nil {
		return vkSession{}, err
	}
	protected, err := os.ReadFile(path)
	if err != nil {
		return vkSession{}, err
	}
	plain, err := unprotectWindowsData(protected)
	if err != nil {
		return vkSession{}, err
	}
	var session vkSession
	if err := json.Unmarshal(plain, &session); err != nil {
		return vkSession{}, err
	}
	return session, nil
}

func deleteVKSession() error {
	path, err := vkSessionFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type windowsDataBlob struct {
	Size uint32
	Data *byte
}

var (
	crypt32DLL             = syscall.NewLazyDLL("crypt32.dll")
	cryptProtectDataProc   = crypt32DLL.NewProc("CryptProtectData")
	cryptUnprotectDataProc = crypt32DLL.NewProc("CryptUnprotectData")
	kernel32DLL            = syscall.NewLazyDLL("kernel32.dll")
	localFreeProc          = kernel32DLL.NewProc("LocalFree")
)

func protectWindowsData(data []byte) ([]byte, error) {
	return callWindowsDataProtection(cryptProtectDataProc, data)
}

func unprotectWindowsData(data []byte) ([]byte, error) {
	return callWindowsDataProtection(cryptUnprotectDataProc, data)
}

func callWindowsDataProtection(proc *syscall.LazyProc, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("пустые данные для Windows DPAPI")
	}
	input := windowsDataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windowsDataBlob
	result, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, errors.New("Windows DPAPI завершился с ошибкой")
	}
	if output.Data == nil || output.Size == 0 {
		return nil, errors.New("Windows DPAPI вернул пустой результат")
	}
	defer localFreeProc.Call(uintptr(unsafe.Pointer(output.Data)))
	resultBytes := make([]byte, int(output.Size))
	copy(resultBytes, unsafe.Slice(output.Data, int(output.Size)))
	return resultBytes, nil
}
