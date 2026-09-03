//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	vkLegacyMobileUserAgent = "Mozilla/5.0 (Linux; Android 14; K; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/131.0.0.0 Mobile Safari/537.36"
	vkEdgeStartupTimeout     = 15 * time.Second
	vkEdgePollInterval       = 350 * time.Millisecond
	vkOAuthHTTPMaxHops       = 12
)

type vkEdgeTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type vkEdgeSession struct {
	cmd       *exec.Cmd
	waitCh    chan error
	conn      *websocket.Conn
	port      int
	nextID    int64
	closed    atomic.Bool
	http      *http.Client
	userAgent string
}

type vkCDPResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type vkCDPEvalResult struct {
	Result struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"result"`
}

type vkCDPPageState struct {
	URL  string `json:"url"`
	Body string `json:"body"`
}

type vkCDPCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

type vkCDPCookiesResult struct {
	Cookies []vkCDPCookie `json:"cookies"`
}

func obtainLegacyVKTokenInWebView(ctx context.Context) (vkLegacyToken, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := clearLegacyVKBrowserProfile(); err != nil {
				return vkLegacyToken{}, fmt.Errorf("не удалось очистить VK-сессию перед повтором: %w", err)
			}
		}

		userAgent := vkLegacyMobileUserAgent
		if attempt >= 2 {
			userAgent = vkLegacyDesktopUserAgent
		}

		session, err := startVKEdgeSession(ctx, legacyVKLoginStartURL(attempt), userAgent)
		if err != nil {
			return vkLegacyToken{}, err
		}

		token, retry, err := runVKEdgeLoginFlow(ctx, session)
		session.close()
		if err == nil {
			return token, nil
		}
		if !retry {
			return vkLegacyToken{}, err
		}
	}

	return vkLegacyToken{}, errors.New("VK ID отклонил все три варианта входа: Unknown method passed")
}

func clearLegacyVKWebViewCookies(context.Context) error {
	return clearLegacyVKBrowserProfile()
}

func runVKEdgeLoginFlow(ctx context.Context, session *vkEdgeSession) (vkLegacyToken, bool, error) {
	phase := "login"

	for {
		select {
		case <-ctx.Done():
			return vkLegacyToken{}, false, context.Canceled
		case <-session.waitCh:
			return vkLegacyToken{}, false, errors.New("окно авторизации VK закрыто")
		case <-time.After(vkEdgePollInterval):
		}

		state, err := session.pageState(ctx)
		if err != nil {
			if session.exited() {
				return vkLegacyToken{}, false, errors.New("окно авторизации VK закрыто")
			}
			continue
		}

		if phase == "login" {
			if strings.Contains(strings.ToLower(state.Body), "unknown method") {
				return vkLegacyToken{}, true, errors.New("VK ID вернул Unknown method passed")
			}

			hasSession, err := session.hasVKSessionCookie(ctx)
			if err != nil {
				continue
			}
			if !hasSession || isLegacyVKIDLoginFlow(state.URL) {
				continue
			}

			// qWDTT first obtains the legacy access token over ordinary HTTP using
			// the already authenticated VK cookies. This avoids navigating the
			// visible browser directly to oauth.vk.com, which currently can end
			// at an oauth.vk.ru HTTP 405 page after VK ID / QR login.
			token, terminal, httpErr := session.obtainLegacyVKAccessTokenViaHTTP(ctx)
			if terminal {
				return token, false, httpErr
			}

			// Keep the same fallback as qWDTT: if the HTTP chain could not obtain
			// a token, try the authorize URL in the authenticated browser session.
			if err := session.navigate(ctx, buildLegacyVKAuthorizeURL()); err != nil {
				if httpErr != nil {
					return vkLegacyToken{}, false, fmt.Errorf("не удалось получить VK OAuth по HTTP (%v) и открыть браузерный fallback: %w", httpErr, err)
				}
				return vkLegacyToken{}, false, fmt.Errorf("не удалось открыть VK OAuth: %w", err)
			}
			phase = "token"
			continue
		}

		token, terminal, parseErr := parseLegacyVKTokenURL(state.URL)
		if terminal {
			return token, false, parseErr
		}
		if strings.Contains(strings.ToLower(state.Body), "http error 405") {
			return vkLegacyToken{}, false, errors.New("VK OAuth browser fallback вернул HTTP 405")
		}
	}
}

func (s *vkEdgeSession) obtainLegacyVKAccessTokenViaHTTP(ctx context.Context) (vkLegacyToken, bool, error) {
	cookieHeader, err := s.vkCookieHeader(ctx)
	if err != nil {
		return vkLegacyToken{}, false, err
	}
	if strings.TrimSpace(cookieHeader) == "" {
		return vkLegacyToken{}, false, errors.New("VK-сессия не содержит cookies для OAuth")
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	currentURL := buildLegacyVKAuthorizeURL()

	for step := 0; step < vkOAuthHTTPMaxHops; step++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return vkLegacyToken{}, false, err
		}
		req.Header.Set("Cookie", cookieHeader)
		req.Header.Set("User-Agent", s.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := client.Do(req)
		if err != nil {
			return vkLegacyToken{}, false, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if readErr != nil {
			return vkLegacyToken{}, false, readErr
		}

		hop := parseLegacyVKAuthorizeHop(currentURL, resp.StatusCode, resp.Header.Get("Location"), string(body))
		if hop.Err != nil {
			return hop.Token, true, hop.Err
		}
		if hop.Token.AccessToken != "" {
			return hop.Token, true, nil
		}
		if strings.TrimSpace(hop.NextURL) == "" {
			return vkLegacyToken{}, false, nil
		}
		currentURL = hop.NextURL
	}

	return vkLegacyToken{}, false, nil
}

func startVKEdgeSession(ctx context.Context, startURL, userAgent string) (*vkEdgeSession, error) {
	edgePath, err := findMicrosoftEdge()
	if err != nil {
		return nil, err
	}

	port, err := reserveLocalPort()
	if err != nil {
		return nil, fmt.Errorf("не удалось подготовить локальный канал управления Edge: %w", err)
	}

	profile := vkEdgeDataPath()
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return nil, fmt.Errorf("не удалось создать профиль VK для Edge: %w", err)
	}

	args := []string{
		"--app=" + startURL,
		"--user-data-dir=" + profile,
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=" + strconv.Itoa(port),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=msEdgeFirstRunExperience",
		"--lang=ru-RU",
		"--user-agent=" + userAgent,
	}
	cmd := exec.Command(edgePath, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("не удалось запустить Microsoft Edge для VK: %w", err)
	}

	session := &vkEdgeSession{
		cmd:       cmd,
		waitCh:    make(chan error, 1),
		port:      port,
		userAgent: userAgent,
		http: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
	go func() {
		session.waitCh <- cmd.Wait()
		session.closed.Store(true)
	}()

	startupCtx, cancel := context.WithTimeout(ctx, vkEdgeStartupTimeout)
	defer cancel()

	target, err := session.waitForPageTarget(startupCtx)
	if err != nil {
		session.close()
		return nil, err
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(startupCtx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		session.close()
		return nil, fmt.Errorf("не удалось подключиться к окну VK в Edge: %w", err)
	}
	session.conn = conn

	if _, err := session.call(startupCtx, "Page.enable", map[string]any{}); err != nil {
		session.close()
		return nil, fmt.Errorf("не удалось включить управление страницей VK: %w", err)
	}
	if _, err := session.call(startupCtx, "Network.enable", map[string]any{}); err != nil {
		session.close()
		return nil, fmt.Errorf("не удалось включить проверку VK-сессии: %w", err)
	}
	return session, nil
}

func (s *vkEdgeSession) waitForPageTarget(ctx context.Context) (vkEdgeTarget, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/list", s.port)
	for {
		select {
		case <-ctx.Done():
			return vkEdgeTarget{}, errors.New("Microsoft Edge не открыл окно VK вовремя")
		case <-s.waitCh:
			return vkEdgeTarget{}, errors.New("Microsoft Edge завершился до открытия VK")
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			resp, reqErr := s.http.Do(req)
			if reqErr == nil {
				var targets []vkEdgeTarget
				decodeErr := json.NewDecoder(resp.Body).Decode(&targets)
				resp.Body.Close()
				if decodeErr == nil {
					for _, target := range targets {
						if target.Type == "page" && target.WebSocketDebuggerURL != "" {
							return target, nil
						}
					}
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (s *vkEdgeSession) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if s.conn == nil {
		return nil, errors.New("Edge DevTools не подключён")
	}

	id := atomic.AddInt64(&s.nextID, 1)
	message := map[string]any{"id": id, "method": method, "params": params}
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(deadline)
	} else {
		_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	}
	if err := s.conn.WriteJSON(message); err != nil {
		return nil, err
	}

	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = s.conn.SetReadDeadline(deadline)
		} else {
			_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		}
		_, payload, err := s.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var response vkCDPResponse
		if err := json.Unmarshal(payload, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("CDP %s: %s", method, response.Error.Message)
		}
		return response.Result, nil
	}
}

func (s *vkEdgeSession) pageState(ctx context.Context) (vkCDPPageState, error) {
	result, err := s.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    `JSON.stringify({url:String(location.href||''),body:(document.body&&document.body.innerText)||''})`,
		"returnByValue": true,
	})
	if err != nil {
		return vkCDPPageState{}, err
	}
	var eval vkCDPEvalResult
	if err := json.Unmarshal(result, &eval); err != nil {
		return vkCDPPageState{}, err
	}
	if eval.Result.Value == "" {
		return vkCDPPageState{}, errors.New("Edge не вернул состояние страницы VK")
	}
	var state vkCDPPageState
	if err := json.Unmarshal([]byte(eval.Result.Value), &state); err != nil {
		return vkCDPPageState{}, err
	}
	return state, nil
}

func (s *vkEdgeSession) getAllCookies(ctx context.Context) ([]vkCDPCookie, error) {
	result, err := s.call(ctx, "Network.getAllCookies", map[string]any{})
	if err != nil {
		return nil, err
	}
	var cookies vkCDPCookiesResult
	if err := json.Unmarshal(result, &cookies); err != nil {
		return nil, err
	}
	return cookies.Cookies, nil
}

func (s *vkEdgeSession) hasVKSessionCookie(ctx context.Context) (bool, error) {
	cookies, err := s.getAllCookies(ctx)
	if err != nil {
		return false, err
	}
	for _, cookie := range cookies {
		if !strings.EqualFold(cookie.Name, "remixsid") || len(strings.TrimSpace(cookie.Value)) < 8 {
			continue
		}
		if isVKCookieDomain(cookie.Domain) {
			return true, nil
		}
	}
	return false, nil
}

func (s *vkEdgeSession) vkCookieHeader(ctx context.Context) (string, error) {
	cookies, err := s.getAllCookies(ctx)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(cookies))
	seen := make(map[string]struct{}, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		value := strings.TrimSpace(cookie.Value)
		if name == "" || value == "" || !isVKCookieDomain(cookie.Domain) {
			continue
		}
		pair := name + "=" + value
		if _, exists := seen[pair]; exists {
			continue
		}
		seen[pair] = struct{}{}
		parts = append(parts, pair)
	}
	return strings.Join(parts, "; "), nil
}

func isVKCookieDomain(raw string) bool {
	domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "."))
	return domain == "vk.ru" || strings.HasSuffix(domain, ".vk.ru") || domain == "vk.com" || strings.HasSuffix(domain, ".vk.com")
}

func (s *vkEdgeSession) navigate(ctx context.Context, targetURL string) error {
	_, err := s.call(ctx, "Page.navigate", map[string]any{"url": targetURL})
	return err
}

func (s *vkEdgeSession) exited() bool {
	return s.closed.Load()
}

func (s *vkEdgeSession) close() {
	if s == nil {
		return
	}
	if s.conn != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = s.call(closeCtx, "Browser.close", map[string]any{})
		cancel()
		_ = s.conn.Close()
	}
	select {
	case <-s.waitCh:
		return
	case <-time.After(2 * time.Second):
	}
	if s.cmd != nil && s.cmd.Process != nil && !s.closed.Load() {
		_ = s.cmd.Process.Kill()
	}
}

func clearLegacyVKBrowserProfile() error {
	path := vkEdgeDataPath()
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

func vkEdgeDataPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "PWDTT", "vk-edge")
}

func findMicrosoftEdge() (string, error) {
	var candidates []string
	if path, err := exec.LookPath("msedge.exe"); err == nil {
		candidates = append(candidates, path)
	}
	for _, root := range []string{os.Getenv("PROGRAMFILES(X86)"), os.Getenv("PROGRAMFILES"), os.Getenv("LOCALAPPDATA")} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("Microsoft Edge не найден; он нужен для входа VK")
}

func reserveLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
