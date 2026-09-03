//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const vkOAuthHTTPMaxHopsRU = 24

var qwdttVKCookieURLs = []string{
	"https://vk.com",
	"https://vk.ru",
	"https://m.vk.com",
	"https://m.vk.ru",
	"https://login.vk.com",
	"https://login.vk.ru",
	"https://oauth.vk.com",
	"https://oauth.vk.ru",
	"https://id.vk.com",
	"https://id.vk.ru",
}

// obtainLegacyVKTokenQWDTT mirrors qWDTT's token sequence after the user has
// already created an ordinary VK browser session. The Edge profile is reused;
// no new VK login is started here.
func obtainLegacyVKTokenQWDTT(ctx context.Context) (vkLegacyToken, error) {
	session, err := startVKEdgeSession(ctx, "https://m.vk.ru/", vkLegacyMobileUserAgent)
	if err != nil {
		return vkLegacyToken{}, err
	}
	defer session.close()

	hasSession, err := session.hasVKSessionCookie(ctx)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("не удалось проверить сохранённую VK-сессию: %w", err)
	}
	if !hasSession {
		return vkLegacyToken{}, errors.New("сохранённая VK-сессия отсутствует; войдите в VK снова")
	}

	token, trace, err := session.obtainLegacyVKAccessTokenQWDTTHTTP(ctx)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth завершился ошибкой (%s): %w", trace, err)
	}
	if token.AccessToken != "" {
		return token, nil
	}

	// qWDTT falls back to an authenticated browser token screen if its HTTP
	// route returns no token. Keep that fallback, but preserve safe HTTP
	// diagnostics so a VK-side change does not collapse into a bare 405.
	if err := session.navigate(ctx, buildLegacyVKAuthorizeURL()); err != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth не вернул токен (%s); не удалось открыть browser fallback: %w", trace, err)
	}

	fallbackCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		select {
		case <-fallbackCtx.Done():
			return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth не вернул токен (%s); browser fallback: timeout", trace)
		case <-session.waitCh:
			return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth не вернул токен (%s); окно browser fallback закрыто", trace)
		case <-time.After(vkEdgePollInterval):
		}

		state, stateErr := session.pageState(fallbackCtx)
		if stateErr != nil {
			if session.exited() {
				return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth не вернул токен (%s); окно browser fallback закрыто", trace)
			}
			continue
		}

		if token, terminal, parseErr := parseLegacyVKTokenURL(state.URL); terminal {
			if parseErr != nil {
				return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth не вернул токен (%s); browser fallback: %w", trace, parseErr)
			}
			return token, nil
		}
		if strings.Contains(strings.ToLower(state.Body), "http error 405") {
			return vkLegacyToken{}, fmt.Errorf("qWDTT HTTP-first OAuth не вернул токен (%s); browser fallback вернул HTTP 405", trace)
		}
	}
}

// qwdttVKCookieHeader is the CDP equivalent of qWDTT's
// CookieManager.getCookie() calls for its fixed VK URL list. Network.getCookies
// returns only cookies applicable to those URLs, unlike Network.getAllCookies.
// VK currently migrates parts of the OAuth chain from *.vk.com to *.vk.ru, so
// the same URL-scoped collection is extended with login.vk.ru/oauth.vk.ru.
func (s *vkEdgeSession) qwdttVKCookieHeader(ctx context.Context) (string, error) {
	result, err := s.call(ctx, "Network.getCookies", map[string]any{"urls": qwdttVKCookieURLs})
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

func (s *vkEdgeSession) obtainLegacyVKAccessTokenQWDTTHTTP(ctx context.Context) (vkLegacyToken, string, error) {
	cookieHeader, err := s.qwdttVKCookieHeader(ctx)
	if err != nil {
		return vkLegacyToken{}, "cookies: unavailable", err
	}
	if strings.TrimSpace(cookieHeader) == "" {
		return vkLegacyToken{}, "cookies: empty", errors.New("VK-сессия не содержит cookies для OAuth")
	}

	client := newQWDTTLegacyOAuthHTTPClient()
	currentURL := buildLegacyVKAuthorizeURL()
	trace := "не выполнено"
	seenURLs := make(map[string]struct{}, vkOAuthHTTPMaxHopsRU)

	for step := 0; step < vkOAuthHTTPMaxHopsRU; step++ {
		if _, exists := seenURLs[currentURL]; exists {
			return vkLegacyToken{}, trace, errors.New("VK OAuth вошёл в цикл redirect")
		}
		seenURLs[currentURL] = struct{}{}

		req, err := newQWDTTLegacyOAuthRequest(ctx, currentURL, cookieHeader, vkLegacyMobileUserAgent)
		if err != nil {
			return vkLegacyToken{}, trace, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return vkLegacyToken{}, trace, err
		}
		body, readErr := readLegacyOAuthBody(resp)
		if readErr != nil {
			return vkLegacyToken{}, trace, readErr
		}

		host := "?"
		if parsed, parseErr := url.Parse(currentURL); parseErr == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		location := resp.Header.Get("Location")
		trace = fmt.Sprintf("шаг %d/%d: HTTP %d, host=%s, redirect=%t", step+1, vkOAuthHTTPMaxHopsRU, resp.StatusCode, host, strings.TrimSpace(location) != "")

		hop := parseLegacyVKAuthorizeHop(currentURL, resp.StatusCode, location, body)
		if hop.Err != nil {
			return hop.Token, trace, hop.Err
		}
		if hop.Token.AccessToken != "" {
			return hop.Token, trace, nil
		}
		if strings.TrimSpace(hop.NextURL) == "" {
			return vkLegacyToken{}, trace, nil
		}
		currentURL = hop.NextURL
	}

	return vkLegacyToken{}, trace, nil
}

func newQWDTTLegacyOAuthHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newQWDTTLegacyOAuthRequest(ctx context.Context, targetURL, cookieHeader, userAgent string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	return req, nil
}

func readLegacyOAuthBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
