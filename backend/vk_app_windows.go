//go:build windows

package backend

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultVKOAuthRedirectURI = "http://127.0.0.1:53682/vk-oauth/callback"
	vkIDAuthorizeURL          = "https://id.vk.ru/authorize"
	vkIDTokenURL              = "https://id.vk.ru/oauth2/auth"
	vkIDLogoutURL             = "https://id.vk.ru/oauth2/logout"
	vkOAuthTimeout            = 5 * time.Minute
)

var (
	vkOAuthClientID     string
	vkOAuthRedirectURI = defaultVKOAuthRedirectURI
)

type vkSession struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	DeviceID     string    `json:"device_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type vkOAuthCallback struct {
	Code             string `json:"code"`
	DeviceID         string `json:"device_id"`
	State            string `json:"state"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type vkOAuthResponseError struct {
	Code        string
	Description string
}

func (e *vkOAuthResponseError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("VK ID: %s", e.Description)
	}
	return fmt.Sprintf("VK ID: %s", e.Code)
}

type vkOAuthTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	DeviceID         string `json:"device_id"`
	State            string `json:"state"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (a *App) IsVKAuthAvailable() bool {
	_, _, err := currentVKOAuthConfig()
	return err == nil
}

func (a *App) IsVKLoggedIn() bool {
	session, err := loadVKSession()
	if err != nil {
		return false
	}
	return session.AccessToken != "" && (session.ExpiresAt.After(time.Now()) || session.RefreshToken != "")
}

func (a *App) VKLogin() error {
	ctx, done, err := a.beginVKOperation()
	if err != nil {
		return err
	}
	defer done()

	clientID, redirectURI, err := currentVKOAuthConfig()
	if err != nil {
		return err
	}
	callbackURL, err := url.Parse(redirectURI)
	if err != nil {
		return errors.New("некорректный redirect URI VK ID")
	}

	listener, err := net.Listen("tcp", callbackURL.Host)
	if err != nil {
		return fmt.Errorf("не удалось открыть локальный OAuth callback: %w", err)
	}
	defer listener.Close()

	verifier, err := randomURLSafe(64)
	if err != nil {
		return errors.New("не удалось подготовить безопасный OAuth-сеанс")
	}
	state, err := randomURLSafe(32)
	if err != nil {
		return errors.New("не удалось подготовить безопасный OAuth-сеанс")
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])

	callbackCh := make(chan vkOAuthCallback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(callbackURL.Path, func(w http.ResponseWriter, r *http.Request) {
		callback, parseErr := parseVKOAuthCallback(r.URL.Query())
		if parseErr != nil {
			http.Error(w, "VK authorization response is invalid", http.StatusBadRequest)
			return
		}
		if callback.State != state {
			http.Error(w, "VK authorization state mismatch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><meta charset=\"utf-8\"><title>PWDTT</title><p>Авторизация VK завершена. Можно закрыть эту вкладку.</p>")
		select {
		case callbackCh <- callback:
		default:
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Shutdown(context.Background())

	authURL := buildVKAuthorizeURL(clientID, redirectURI, state, challenge)
	wails.BrowserOpenURL(a.ctx, authURL)

	timer := time.NewTimer(vkOAuthTimeout)
	defer timer.Stop()
	var callback vkOAuthCallback
	select {
	case <-ctx.Done():
		return errors.New("операция VK отменена")
	case <-timer.C:
		return errors.New("время ожидания авторизации VK истекло")
	case callback = <-callbackCh:
	}
	if callback.Error != "" {
		if callback.ErrorDescription != "" {
			return fmt.Errorf("авторизация VK отклонена: %s", callback.ErrorDescription)
		}
		return fmt.Errorf("авторизация VK отклонена: %s", callback.Error)
	}
	if callback.Code == "" || callback.DeviceID == "" {
		return errors.New("VK ID не вернул код авторизации")
	}

	token, err := exchangeVKAuthorizationCode(ctx, clientID, redirectURI, verifier, callback)
	if err != nil {
		return err
	}
	session := vkSession{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		DeviceID:     firstNonEmpty(token.DeviceID, callback.DeviceID),
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
	}
	if session.AccessToken == "" {
		return errors.New("VK ID не вернул access token")
	}
	if token.ExpiresIn <= 0 {
		session.ExpiresAt = time.Now().Add(time.Hour)
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

	if session, loadErr := loadVKSession(); loadErr == nil && session.AccessToken != "" {
		if clientID, _, configErr := currentVKOAuthConfig(); configErr == nil {
			_ = revokeVKSession(ctx, clientID, session.AccessToken)
		}
	}
	if err := deleteVKSession(); err != nil {
		return fmt.Errorf("не удалось удалить локальную авторизацию VK: %w", err)
	}
	a.onBridgeEvent("vk-auth-changed", false)
	return nil
}

func (a *App) GenerateVKHashes(count int, existing []string) ([]string, error) {
	ctx, done, err := a.beginVKOperation()
	if err != nil {
		return nil, err
	}
	defer done()

	accessToken, err := validVKAccessToken(ctx)
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

func currentVKOAuthConfig() (string, string, error) {
	clientID := strings.TrimSpace(vkOAuthClientID)
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv("PWDTT_VK_APP_ID"))
	}
	redirectURI := strings.TrimSpace(vkOAuthRedirectURI)
	if envRedirect := strings.TrimSpace(os.Getenv("PWDTT_VK_REDIRECT_URI")); envRedirect != "" {
		redirectURI = envRedirect
	}
	if clientID == "" {
		return "", "", errors.New("VK ID не настроен в этой сборке")
	}
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path == "" {
		return "", "", errors.New("VK ID redirect URI должен быть локальным HTTP URL")
	}
	host := strings.ToLower(u.Hostname())
	if host != "127.0.0.1" && host != "localhost" {
		return "", "", errors.New("VK ID redirect URI должен вести на localhost")
	}
	if u.Port() == "" {
		return "", "", errors.New("VK ID redirect URI должен содержать фиксированный порт")
	}
	return clientID, redirectURI, nil
}

func buildVKAuthorizeURL(clientID, redirectURI, state, challenge string) string {
	query := url.Values{
		"client_id":             {clientID},
		"app_id":                {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"s256"},
		"state":                 {state},
		"scope":                 {"messages"},
	}
	return vkIDAuthorizeURL + "?" + query.Encode()
}

func parseVKOAuthCallback(values url.Values) (vkOAuthCallback, error) {
	if payload := values.Get("payload"); payload != "" {
		var callback vkOAuthCallback
		if err := json.Unmarshal([]byte(payload), &callback); err != nil {
			return vkOAuthCallback{}, err
		}
		return callback, nil
	}
	return vkOAuthCallback{
		Code:             values.Get("code"),
		DeviceID:         values.Get("device_id"),
		State:            values.Get("state"),
		Error:            values.Get("error"),
		ErrorDescription: values.Get("error_description"),
	}, nil
}

func exchangeVKAuthorizationCode(ctx context.Context, clientID, redirectURI, verifier string, callback vkOAuthCallback) (vkOAuthTokenResponse, error) {
	query := url.Values{
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"state":         {callback.State},
		"device_id":     {callback.DeviceID},
	}
	return requestVKToken(ctx, query, url.Values{"code": {callback.Code}}, callback.State)
}

func refreshVKSession(ctx context.Context, clientID, redirectURI string, session vkSession) (vkSession, error) {
	state, err := randomURLSafe(32)
	if err != nil {
		return vkSession{}, errors.New("не удалось обновить авторизацию VK")
	}
	query := url.Values{
		"grant_type":   {"refresh_token"},
		"redirect_uri": {redirectURI},
		"client_id":    {clientID},
		"device_id":    {session.DeviceID},
		"state":        {state},
	}
	token, err := requestVKToken(ctx, query, url.Values{"refresh_token": {session.RefreshToken}}, state)
	if err != nil {
		return vkSession{}, err
	}
	if token.AccessToken == "" {
		return vkSession{}, errors.New("VK ID не вернул новый access token")
	}
	refreshed := vkSession{
		AccessToken:  token.AccessToken,
		RefreshToken: firstNonEmpty(token.RefreshToken, session.RefreshToken),
		DeviceID:     firstNonEmpty(token.DeviceID, session.DeviceID),
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
	}
	if token.ExpiresIn <= 0 {
		refreshed.ExpiresAt = time.Now().Add(time.Hour)
	}
	return refreshed, nil
}

func requestVKToken(ctx context.Context, query, form url.Values, expectedState string) (vkOAuthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vkIDTokenURL+"?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return vkOAuthTokenResponse{}, errors.New("не удалось подготовить запрос VK ID")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return vkOAuthTokenResponse{}, context.Canceled
		}
		return vkOAuthTokenResponse{}, fmt.Errorf("VK ID недоступен: %w", err)
	}
	defer resp.Body.Close()

	var token vkOAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return vkOAuthTokenResponse{}, errors.New("VK ID вернул некорректный ответ")
	}
	if token.Error != "" {
		return vkOAuthTokenResponse{}, &vkOAuthResponseError{Code: token.Error, Description: token.ErrorDescription}
	}
	if token.State != "" && token.State != expectedState {
		return vkOAuthTokenResponse{}, errors.New("VK ID вернул ответ с неверным state")
	}
	return token, nil
}

func validVKAccessToken(ctx context.Context) (string, error) {
	session, err := loadVKSession()
	if err != nil || session.AccessToken == "" {
		return "", errors.New("сначала войдите в VK")
	}
	if session.ExpiresAt.After(time.Now().Add(30 * time.Second)) {
		return session.AccessToken, nil
	}
	if session.RefreshToken == "" || session.DeviceID == "" {
		_ = deleteVKSession()
		return "", errors.New("авторизация VK истекла; войдите снова")
	}
	clientID, redirectURI, err := currentVKOAuthConfig()
	if err != nil {
		return "", err
	}
	refreshed, err := refreshVKSession(ctx, clientID, redirectURI, session)
	if err != nil {
		var oauthErr *vkOAuthResponseError
		if errors.As(err, &oauthErr) {
			_ = deleteVKSession()
		}
		return "", fmt.Errorf("не удалось обновить авторизацию VK: %w", err)
	}
	if err := saveVKSession(refreshed); err != nil {
		return "", fmt.Errorf("не удалось сохранить обновлённую авторизацию VK: %w", err)
	}
	return refreshed.AccessToken, nil
}

func revokeVKSession(ctx context.Context, clientID, accessToken string) error {
	query := url.Values{"client_id": {clientID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vkIDLogoutURL+"?"+query.Encode(), strings.NewReader(url.Values{"access_token": {accessToken}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func randomURLSafe(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
