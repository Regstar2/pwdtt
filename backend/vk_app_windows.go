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
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

func (a *App) IsVKAuthAvailable() bool {
	return true
}

func (a *App) IsVKLoggedIn() bool {
	session, err := loadVKSession()
	if err != nil || session.AccessToken == "" {
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

	token, err := obtainLegacyVKTokenInWebView(loginCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(loginCtx.Err(), context.DeadlineExceeded) {
			return errors.New("время ожидания авторизации VK истекло")
		}
		if errors.Is(err, context.Canceled) {
			return errors.New("операция VK отменена")
		}
		return err
	}
	if token.AccessToken == "" {
		return errors.New("VK OAuth не вернул access token")
	}

	session := vkSession{AccessToken: token.AccessToken}
	if token.ExpiresIn > 0 {
		session.ExpiresAt = time.Now().Add(token.ExpiresIn)
	}
	if err := saveVKSession(session); err != nil {
		return fmt.Errorf("не удалось сохранить авторизацию VK: %w", err)
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
	if err := clearLegacyVKWebViewCookies(clearCtx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(clearCtx.Err(), context.DeadlineExceeded) {
			return errors.New("локальный токен удалён, но не удалось очистить cookies VK")
		}
		return fmt.Errorf("локальный токен удалён, но cookies VK не очищены: %w", err)
	}
	return nil
}

func (a *App) GenerateVKHashes(count int, existing []string) ([]string, error) {
	ctx, done, err := a.beginVKOperation()
	if err != nil {
		return nil, err
	}
	defer done()

	accessToken, err := validVKAccessToken()
	if err != nil {
		return nil, err
	}
	hashes, err := generateVKHashes(ctx, a.vkClient, accessToken, count, existing, func(hash string, current, total int) {
		a.onBridgeEvent("vk-hash-generated", hash, current, total)
	})
	if err != nil {
		var apiErr *vkAPIError
		if errors.As(err, &apiErr) && apiErr.Code == 5 {
			_ = deleteVKSession()
			a.onBridgeEvent("vk-auth-changed", false)
			return hashes, errors.New("авторизация VK истекла или была отозвана; войдите снова")
		}
		if errors.Is(err, context.Canceled) {
			return hashes, errors.New("операция VK отменена")
		}
		return hashes, err
	}
	return hashes, nil
}

func validVKAccessToken() (string, error) {
	session, err := loadVKSession()
	if err != nil || session.AccessToken == "" {
		return "", errors.New("сначала войдите в VK")
	}
	if !session.ExpiresAt.IsZero() && !session.ExpiresAt.After(time.Now().Add(30*time.Second)) {
		_ = deleteVKSession()
		return "", errors.New("авторизация VK истекла; войдите снова")
	}
	return session.AccessToken, nil
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
