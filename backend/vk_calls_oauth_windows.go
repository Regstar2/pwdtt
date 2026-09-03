//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	vkCallsOAuthClientID       = "7793118"
	vkCallsOAuthScope          = "1073737727"
	vkCallsOAuthRedirectURI    = "https://oauth.vk.ru/blank.html"
	vkCallsOAuthHTTPAuthorize  = "https://oauth.vk.com/authorize"
	vkCallsOAuthWebAuthorize   = "https://oauth.vk.ru/authorize"
	vkCallsOAuthMaxHops        = 15
	vkCallsOAuthBrowserTimeout = 60 * time.Second
)

var vkCallsOAuthCookieURLs = []string{
	"https://vk.com/",
	"https://vk.ru/",
	"https://id.vk.ru/",
	"https://id.vk.com/",
	"https://login.vk.com/",
	"https://login.vk.ru/",
	"https://m.vk.com/",
	"https://m.vk.ru/",
}

// obtainVKCallsTokenPrimary mirrors the current auto-API token flow used by
// csqtt/qWDTT-compatible clients for VK calls. It deliberately avoids the
// auth.getOauthToken migration path that current VK can reject with invalid
// hash for client 6287487.
func obtainVKCallsTokenPrimary(ctx context.Context) (vkLegacyToken, error) {
	session, err := startVKEdgeSession(ctx, "https://m.vk.ru/", vkLegacyMobileUserAgent)
	if err != nil {
		return vkLegacyToken{}, err
	}
	defer session.close()

	hasSession, err := session.hasVKSessionCookie(ctx)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("не удалось проверить VK-сессию: %w", err)
	}
	if !hasSession {
		return vkLegacyToken{}, errors.New("сохранённая VK-сессия отсутствует; войдите в VK снова")
	}

	cookieHeader, err := session.vkCallsOAuthCookieHeader(ctx)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("не удалось получить cookies VK Calls OAuth: %w", err)
	}
	if strings.TrimSpace(cookieHeader) == "" {
		return vkLegacyToken{}, errors.New("VK-сессия не содержит cookies для VK Calls OAuth")
	}

	if token, scrapeErr := scrapeVKCallsTokenHTTP(ctx, cookieHeader, vkLegacyMobileUserAgent); scrapeErr == nil && token.AccessToken != "" {
		return token, nil
	}

	return session.obtainVKCallsTokenViaBrowser(ctx)
}

func buildVKCallsHTTPAuthorizeURL() string {
	query := url.Values{
		"client_id":     {vkCallsOAuthClientID},
		"display":       {"mobile"},
		"redirect_uri":  {vkCallsOAuthRedirectURI},
		"response_type": {"token"},
		"scope":         {vkCallsOAuthScope},
		"v":             {vkAPIVersion},
		"revoke":        {"1"},
	}
	return vkCallsOAuthHTTPAuthorize + "?" + query.Encode()
}

func buildVKCallsBrowserAuthorizeURL() string {
	query := url.Values{
		"client_id":     {vkCallsOAuthClientID},
		"scope":         {vkCallsOAuthScope},
		"redirect_uri":  {vkCallsOAuthRedirectURI},
		"display":       {"page"},
		"response_type": {"token"},
		"revoke":        {"1"},
		"v":             {vkAPIVersion},
	}
	return vkCallsOAuthWebAuthorize + "?" + query.Encode()
}

func (s *vkEdgeSession) vkCallsOAuthCookieHeader(ctx context.Context) (string, error) {
	result, err := s.call(ctx, "Network.getCookies", map[string]any{"urls": vkCallsOAuthCookieURLs})
	if err != nil {
		return "", err
	}
	var cookies vkCDPCookiesResult
	if err := json.Unmarshal(result, &cookies); err != nil {
		return "", err
	}

	parts := make([]string, 0, len(cookies.Cookies))
	seen := make(map[string]struct{}, len(cookies.Cookies))
	for _, cookie := range cookies.Cookies {
		name := strings.TrimSpace(cookie.Name)
		value := strings.TrimSpace(cookie.Value)
		if name == "" || value == "" {
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

func scrapeVKCallsTokenHTTP(ctx context.Context, cookieHeader, userAgent string) (vkLegacyToken, error) {
	currentURL := buildVKCallsHTTPAuthorizeURL()
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for hop := 0; hop < vkCallsOAuthMaxHops; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return vkLegacyToken{}, err
		}
		req.Header.Set("Cookie", cookieHeader)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := client.Do(req)
		if err != nil {
			return vkLegacyToken{}, err
		}
		body, readErr := readLegacyOAuthBody(resp)
		if readErr != nil {
			return vkLegacyToken{}, readErr
		}

		hopResult := parseLegacyVKAuthorizeHop(currentURL, resp.StatusCode, resp.Header.Get("Location"), body)
		if hopResult.Err != nil {
			return vkLegacyToken{}, hopResult.Err
		}
		if hopResult.Token.AccessToken != "" {
			return hopResult.Token, nil
		}
		if strings.TrimSpace(hopResult.NextURL) == "" {
			return vkLegacyToken{}, fmt.Errorf("VK Calls OAuth HTTP scraper остановился на шаге %d: HTTP %d", hop+1, resp.StatusCode)
		}
		currentURL = hopResult.NextURL
	}

	return vkLegacyToken{}, errors.New("VK Calls OAuth HTTP scraper превысил лимит redirect")
}

func (s *vkEdgeSession) obtainVKCallsTokenViaBrowser(ctx context.Context) (vkLegacyToken, error) {
	if err := s.navigate(ctx, buildVKCallsBrowserAuthorizeURL()); err != nil {
		return vkLegacyToken{}, fmt.Errorf("не удалось открыть VK Calls OAuth: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, vkCallsOAuthBrowserTimeout)
	defer cancel()

	silentRetried := false
	lastDiag := "страница не прочитана"
	for {
		select {
		case <-waitCtx.Done():
			return vkLegacyToken{}, fmt.Errorf("VK Calls OAuth не вернул access token (%s)", lastDiag)
		case <-s.waitCh:
			return vkLegacyToken{}, fmt.Errorf("окно VK Calls OAuth закрыто (%s)", lastDiag)
		case <-time.After(vkEdgePollInterval):
		}

		state, err := s.pageState(waitCtx)
		if err != nil {
			if s.exited() {
				return vkLegacyToken{}, errors.New("окно VK Calls OAuth закрыто")
			}
			continue
		}

		if token, terminal, parseErr := parseLegacyVKTokenURL(state.URL); terminal {
			if parseErr != nil {
				return vkLegacyToken{}, parseErr
			}
			if token.AccessToken != "" {
				return token, nil
			}
		}
		if token, ok := parseModernRUAccessTokenURL(state.URL); ok && token.AccessToken != "" {
			return token, nil
		}

		parsed, _ := url.Parse(strings.TrimSpace(state.URL))
		host := parsed.Hostname()
		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}
		lastDiag = fmt.Sprintf("host=%s path=%s", host, path)

		fragmentValues, _ := url.ParseQuery(parsed.Fragment)
		if fragmentValues.Get("payload") != "" && !silentRetried {
			silentRetried = true
			timer := time.NewTimer(1500 * time.Millisecond)
			select {
			case <-waitCtx.Done():
				timer.Stop()
				return vkLegacyToken{}, waitCtx.Err()
			case <-timer.C:
			}
			if err := s.navigate(waitCtx, buildVKCallsBrowserAuthorizeURL()); err != nil {
				return vkLegacyToken{}, err
			}
			continue
		}

		if isVKLoginRateLimitedText(state.Body) {
			return vkLegacyToken{}, errors.New("VK временно ограничил авторизацию: слишком много попыток; повторите позже")
		}
	}
}

func obtainVKTokenWithFallback(ctx context.Context) (vkLegacyToken, error) {
	token, primaryErr := obtainVKCallsTokenPrimary(ctx)
	if primaryErr == nil && token.AccessToken != "" {
		return token, nil
	}

	legacyToken, legacyErr := obtainLegacyVKTokenAdaptiveV2(ctx)
	if legacyErr == nil && legacyToken.AccessToken != "" {
		return legacyToken, nil
	}

	if primaryErr == nil {
		primaryErr = errors.New("VK Calls OAuth завершился без access token")
	}
	if legacyErr == nil {
		legacyErr = errors.New("legacy qWDTT OAuth завершился без access token")
	}
	return vkLegacyToken{}, fmt.Errorf("VK Calls OAuth: %v; legacy qWDTT fallback: %v", primaryErr, legacyErr)
}
