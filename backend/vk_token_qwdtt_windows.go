//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var qwdttVKCookieURLs = []string{
	"https://vk.com",
	"https://vk.ru",
	"https://m.vk.com",
	"https://m.vk.ru",
	"https://login.vk.com",
	"https://oauth.vk.com",
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

	for step := 0; step < vkOAuthHTTPMaxHops; step++ {
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
		trace = fmt.Sprintf("шаг %d/%d: HTTP %d, host=%s, redirect=%t", step+1, vkOAuthHTTPMaxHops, resp.StatusCode, host, strings.TrimSpace(location) != "")

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
