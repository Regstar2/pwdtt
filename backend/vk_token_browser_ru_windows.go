//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const vkModernRUBrowserWait = 25 * time.Second

var (
	vkModernWindowInitRE      = regexp.MustCompile(`(?s)window\.init\s*=\s*({.*?});`)
	vkModernReturnAuthHashRE = regexp.MustCompile(`(?s)"return_auth_hash"\s*:\s*"([^"\\]+)"`)
)

// obtainLegacyVKTokenAdaptiveV2 keeps the qWDTT account OAuth route first. If
// current VK migrates that route into the observed login.vk.ru redirect loop,
// let the already-authenticated Edge profile execute the modern .ru authorize
// page. This allows VK itself to establish any browser-only login cookies before
// PWDTT performs connect_internal/auth.getOauthToken in the backend.
func obtainLegacyVKTokenAdaptiveV2(ctx context.Context) (vkLegacyToken, error) {
	token, err := obtainLegacyVKTokenQWDTT(ctx)
	if err == nil {
		return token, nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "цикл redirect") {
		return vkLegacyToken{}, err
	}

	session, sessionErr := startVKEdgeSession(ctx, "https://m.vk.ru/", vkLegacyMobileUserAgent)
	if sessionErr != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT OAuth зациклился; не удалось открыть VK .ru browser fallback: %w", sessionErr)
	}
	defer session.close()

	hasSession, sessionErr := session.hasVKSessionCookie(ctx)
	if sessionErr != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT OAuth зациклился; не удалось проверить VK-сессию: %w", sessionErr)
	}
	if !hasSession {
		return vkLegacyToken{}, errors.New("qWDTT OAuth зациклился, а сохранённая VK-сессия отсутствует")
	}

	browserToken, browserErr := session.obtainModernRUAuthorizeViaBrowser(ctx)
	if browserErr != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT OAuth зациклился; VK .ru browser fallback: %w", browserErr)
	}
	return browserToken, nil
}

func (s *vkEdgeSession) obtainModernRUAuthorizeViaBrowser(ctx context.Context) (vkLegacyToken, error) {
	if err := s.navigate(ctx, buildModernRUAuthorizeURL()); err != nil {
		return vkLegacyToken{}, fmt.Errorf("не удалось открыть oauth.vk.ru: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, vkModernRUBrowserWait)
	defer cancel()

	lastDiag := "страница не прочитана"
	triedHTTPAfterPCookie := false
	for {
		select {
		case <-waitCtx.Done():
			return vkLegacyToken{}, fmt.Errorf("oauth.vk.ru не вернул token/return_auth (%s)", lastDiag)
		case <-s.waitCh:
			return vkLegacyToken{}, fmt.Errorf("окно oauth.vk.ru закрыто (%s)", lastDiag)
		case <-time.After(vkEdgePollInterval):
		}

		state, err := s.pageState(waitCtx)
		if err != nil {
			if s.exited() {
				return vkLegacyToken{}, errors.New("окно oauth.vk.ru закрыто")
			}
			continue
		}

		if token, ok := parseModernRUAccessTokenURL(state.URL); ok {
			return token, nil
		}

		returnAuthHash := extractModernRUReturnAuthHashFlexible(state.Body)
		if returnAuthHash == "" {
			returnAuthHash = extractModernRUReturnAuthHashFromURL(state.URL)
		}
		if returnAuthHash != "" {
			return s.exchangeModernRUReturnAuthHash(waitCtx, returnAuthHash)
		}

		hasP, _ := s.hasModernRULoginCookieP(waitCtx)
		lastDiag = safeModernRUPageDiag(state.URL, state.Body, hasP)

		// python273/vk_api explicitly requires the `p` cookie for current .ru
		// API auth. Once the browser has created it, retry authorize once over
		// backend HTTP with browser-like headers and the freshly updated cookies.
		if hasP && !triedHTTPAfterPCookie {
			triedHTTPAfterPCookie = true
			if token, hash, httpDiag, httpErr := s.fetchModernRUAuthorizeMaterialHTTP(waitCtx); httpErr == nil {
				if token.AccessToken != "" {
					return token, nil
				}
				if hash != "" {
					return s.exchangeModernRUReturnAuthHash(waitCtx, hash)
				}
				lastDiag += "; http=" + httpDiag
			} else {
				lastDiag += "; http_error=" + httpErr.Error()
			}
		}

		lowerBody := strings.ToLower(state.Body)
		if strings.Contains(lowerBody, "http error 405") {
			return vkLegacyToken{}, fmt.Errorf("oauth.vk.ru browser page вернул HTTP 405 (%s)", lastDiag)
		}
	}
}

func (s *vkEdgeSession) fetchModernRUAuthorizeMaterialHTTP(ctx context.Context) (vkLegacyToken, string, string, error) {
	client, err := s.newModernRUVKHTTPClient(ctx)
	if err != nil {
		return vkLegacyToken{}, "", "client", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildModernRUAuthorizeURL(), nil)
	if err != nil {
		return vkLegacyToken{}, "", "request", err
	}
	req.Header.Set("User-Agent", vkLegacyDesktopUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Referer", "https://vk.ru/")

	resp, err := client.Do(req)
	if err != nil {
		return vkLegacyToken{}, "", "request", err
	}
	body, err := readLegacyOAuthBody(resp)
	if err != nil {
		return vkLegacyToken{}, "", "response", err
	}

	finalURL := ""
	status := resp.StatusCode
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if token, ok := parseModernRUAccessTokenURL(finalURL); ok {
		return token, "", safeModernRUHTTPDiag(finalURL, status, true), nil
	}
	hash := extractModernRUReturnAuthHashFromURL(finalURL)
	if hash == "" {
		hash = extractModernRUReturnAuthHashFlexible(body)
	}
	return vkLegacyToken{}, hash, safeModernRUHTTPDiag(finalURL, status, hash != ""), nil
}

func (s *vkEdgeSession) exchangeModernRUReturnAuthHash(ctx context.Context, returnAuthHash string) (vkLegacyToken, error) {
	returnAuthHash = strings.TrimSpace(returnAuthHash)
	if returnAuthHash == "" {
		return vkLegacyToken{}, errors.New("пустой return_auth hash")
	}

	client, err := s.newModernRUVKHTTPClient(ctx)
	if err != nil {
		return vkLegacyToken{}, err
	}

	connectForm := url.Values{
		"uuid":             {""},
		"service_group":    {""},
		"return_auth_hash": {returnAuthHash},
		"version":          {"1"},
		"app_id":           {vkLegacyClientID},
	}
	connectReq, err := http.NewRequestWithContext(ctx, http.MethodPost, vkModernRUConnectInternal, strings.NewReader(connectForm.Encode()))
	if err != nil {
		return vkLegacyToken{}, err
	}
	connectReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	connectReq.Header.Set("Origin", "https://id.vk.ru")
	connectReq.Header.Set("Referer", "https://id.vk.ru/")
	connectReq.Header.Set("User-Agent", vkLegacyDesktopUserAgent)

	connectResp, err := client.Do(connectReq)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("VK connect_internal: %w", err)
	}
	connectBody, err := io.ReadAll(io.LimitReader(connectResp.Body, 1<<20))
	connectResp.Body.Close()
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("VK connect_internal response: %w", err)
	}

	var connectData struct {
		Type string `json:"type"`
		Data struct {
			AccessToken  string `json:"access_token"`
			AuthUserHash string `json:"auth_user_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(connectBody, &connectData); err != nil {
		return vkLegacyToken{}, errors.New("VK connect_internal вернул некорректный ответ")
	}
	if connectData.Type != "okay" || strings.TrimSpace(connectData.Data.AccessToken) == "" || strings.TrimSpace(connectData.Data.AuthUserHash) == "" {
		return vkLegacyToken{}, errors.New("VK connect_internal не подтвердил seamless OAuth")
	}

	tokenForm := buildModernRUGetOAuthTokenForm(
		returnAuthHash,
		connectData.Data.AuthUserHash,
		connectData.Data.AccessToken,
	)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, vkModernRUGetOAuthTokenURL, strings.NewReader(tokenForm.Encode()))
	if err != nil {
		return vkLegacyToken{}, err
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.Header.Set("User-Agent", vkLegacyDesktopUserAgent)

	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("VK auth.getOauthToken: %w", err)
	}
	tokenBody, err := io.ReadAll(io.LimitReader(tokenResp.Body, 1<<20))
	tokenResp.Body.Close()
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("VK auth.getOauthToken response: %w", err)
	}

	var tokenData struct {
		Response struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		} `json:"response"`
		Error struct {
			Code    int    `json:"error_code"`
			Message string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(tokenBody, &tokenData); err != nil {
		return vkLegacyToken{}, errors.New("VK auth.getOauthToken вернул некорректный ответ")
	}
	if tokenData.Error.Code != 0 {
		message := strings.TrimSpace(tokenData.Error.Message)
		if message == "" {
			message = "VK API error"
		}
		return vkLegacyToken{}, fmt.Errorf("VK auth.getOauthToken: %s (%d)", message, tokenData.Error.Code)
	}
	if strings.TrimSpace(tokenData.Response.AccessToken) == "" {
		return vkLegacyToken{}, errors.New("VK auth.getOauthToken не вернул access token")
	}

	var expires time.Duration
	if tokenData.Response.ExpiresIn > 0 {
		expires = time.Duration(tokenData.Response.ExpiresIn) * time.Second
	}
	return vkLegacyToken{AccessToken: tokenData.Response.AccessToken, ExpiresIn: expires}, nil
}

func (s *vkEdgeSession) hasModernRULoginCookieP(ctx context.Context) (bool, error) {
	cookies, err := s.getAllCookies(ctx)
	if err != nil {
		return false, err
	}
	for _, cookie := range cookies {
		if !strings.EqualFold(strings.TrimSpace(cookie.Name), "p") || strings.TrimSpace(cookie.Value) == "" {
			continue
		}
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cookie.Domain), "."))
		if domain == "login.vk.ru" || strings.HasSuffix(domain, ".login.vk.ru") {
			return true, nil
		}
	}
	return false, nil
}

func extractModernRUReturnAuthHashFlexible(body string) string {
	body = html.UnescapeString(body)
	if hash := extractModernRUReturnAuthHash(body); hash != "" {
		return hash
	}
	if match := vkModernReturnAuthHashRE.FindStringSubmatch(body); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if match := vkModernWindowInitRE.FindStringSubmatch(body); len(match) > 1 {
		var payload any
		if json.Unmarshal([]byte(match[1]), &payload) == nil {
			if hash := findJSONStringByKey(payload, "return_auth"); hash != "" {
				return hash
			}
			if hash := findJSONStringByKey(payload, "return_auth_hash"); hash != "" {
				return hash
			}
		}
	}
	return ""
}

func findJSONStringByKey(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			if k == key {
				if s, ok := v.(string); ok {
					return strings.TrimSpace(s)
				}
			}
			if found := findJSONStringByKey(v, key); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findJSONStringByKey(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func safeModernRUPageDiag(rawURL, body string, hasP bool) string {
	parsed, _ := url.Parse(strings.TrimSpace(rawURL))
	host := parsed.Hostname()
	if host == "" {
		host = "?"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	keys := make([]string, 0, len(parsed.Query()))
	for key := range parsed.Query() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lower := strings.ToLower(body)
	return fmt.Sprintf("host=%s path=%s query_keys=%s p_cookie=%t window_init=%t return_auth_marker=%t",
		host, path, strings.Join(keys, ","), hasP,
		strings.Contains(lower, "window.init"),
		strings.Contains(lower, "return_auth"),
	)
}

func safeModernRUHTTPDiag(rawURL string, status int, foundMaterial bool) string {
	parsed, _ := url.Parse(strings.TrimSpace(rawURL))
	host := parsed.Hostname()
	if host == "" {
		host = "?"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return fmt.Sprintf("HTTP %d host=%s path=%s material=%t", status, host, path, foundMaterial)
}
