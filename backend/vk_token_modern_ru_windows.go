//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	vkModernRUAuthorizeURL      = "https://oauth.vk.ru/authorize"
	vkModernRUConnectInternal  = "https://login.vk.ru/?act=connect_internal"
	vkModernRUGetOAuthTokenURL = "https://api.vk.ru/method/auth.getOauthToken"
	vkModernRUAPIVersion       = "5.207"
)

var vkModernReturnAuthRE = regexp.MustCompile(`(?s)"return_auth"\s*:\s*"([^"\\]+)"`)

// obtainLegacyVKTokenAdaptive keeps qWDTT as the primary token path. Current
// VK can migrate that legacy GET chain to login.vk.ru and loop there; only for
// that observed condition do we switch to VK's current seamless .ru exchange.
func obtainLegacyVKTokenAdaptive(ctx context.Context) (vkLegacyToken, error) {
	token, err := obtainLegacyVKTokenQWDTT(ctx)
	if err == nil {
		return token, nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "цикл redirect") {
		return vkLegacyToken{}, err
	}

	session, sessionErr := startVKEdgeSession(ctx, "https://m.vk.ru/", vkLegacyMobileUserAgent)
	if sessionErr != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT OAuth зациклился; не удалось открыть VK .ru fallback: %w", sessionErr)
	}
	defer session.close()

	hasSession, sessionErr := session.hasVKSessionCookie(ctx)
	if sessionErr != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT OAuth зациклился; не удалось проверить VK-сессию: %w", sessionErr)
	}
	if !hasSession {
		return vkLegacyToken{}, errors.New("qWDTT OAuth зациклился, а сохранённая VK-сессия отсутствует")
	}

	modernToken, modernErr := session.obtainLegacyVKTokenViaModernRU(ctx)
	if modernErr != nil {
		return vkLegacyToken{}, fmt.Errorf("qWDTT OAuth зациклился; VK .ru seamless fallback: %w", modernErr)
	}
	return modernToken, nil
}

// obtainLegacyVKTokenViaModernRU handles the current VK *.ru seamless-auth
// transition used after a valid browser session. The application id and
// requested permission remain the qWDTT values (6287487 / messages).
func (s *vkEdgeSession) obtainLegacyVKTokenViaModernRU(ctx context.Context) (vkLegacyToken, error) {
	client, err := s.newModernRUVKHTTPClient(ctx)
	if err != nil {
		return vkLegacyToken{}, err
	}

	resp, err := client.Get(buildModernRUAuthorizeURL())
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("VK .ru authorize: %w", err)
	}
	body, err := readLegacyOAuthBody(resp)
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("VK .ru authorize response: %w", err)
	}

	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if token, ok := parseModernRUAccessTokenURL(finalURL); ok {
		return token, nil
	}

	returnAuthHash := extractModernRUReturnAuthHashFromURL(finalURL)
	if returnAuthHash == "" {
		returnAuthHash = extractModernRUReturnAuthHash(body)
	}
	if returnAuthHash == "" {
		return vkLegacyToken{}, errors.New("VK .ru authorize не вернул return_auth hash")
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
	connectReq.Header.Set("User-Agent", vkLegacyMobileUserAgent)

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
	tokenReq.Header.Set("User-Agent", vkLegacyMobileUserAgent)

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

func buildModernRUAuthorizeURL() string {
	query := url.Values{
		"client_id":     {vkLegacyClientID},
		"scope":         {"messages"},
		"response_type": {"token"},
	}
	return vkModernRUAuthorizeURL + "?" + query.Encode()
}

func extractModernRUReturnAuthHash(body string) string {
	match := vkModernReturnAuthRE.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func extractModernRUReturnAuthHashFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("return_auth_hash"))
}

func parseModernRUAccessTokenURL(raw string) (vkLegacyToken, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return vkLegacyToken{}, false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return vkLegacyToken{}, false
	}

	// Current VK can return a service access_token query parameter together
	// with authorize_url. The real user token is inside authorize_url's
	// fragment, so always prefer that nested URL when it is present.
	authorizeURL := strings.TrimSpace(parsed.Query().Get("authorize_url"))
	if authorizeURL != "" {
		for i := 0; i < 2; i++ {
			decoded, decodeErr := url.QueryUnescape(authorizeURL)
			if decodeErr != nil || decoded == authorizeURL {
				break
			}
			authorizeURL = decoded
		}
		if token, terminal, parseErr := parseLegacyVKTokenURL(authorizeURL); terminal && parseErr == nil && token.AccessToken != "" {
			return token, true
		}
	}

	if token, terminal, parseErr := parseLegacyVKTokenURL(raw); terminal && parseErr == nil && token.AccessToken != "" {
		return token, true
	}
	return vkLegacyToken{}, false
}

func (s *vkEdgeSession) newModernRUVKHTTPClient(ctx context.Context) (*http.Client, error) {
	cookies, err := s.getAllCookies(ctx)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		value := strings.TrimSpace(cookie.Value)
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cookie.Domain), "."))
		if name == "" || value == "" || domain == "" {
			continue
		}
		if domain != "vk.ru" && !strings.HasSuffix(domain, ".vk.ru") && domain != "vk.com" && !strings.HasSuffix(domain, ".vk.com") {
			continue
		}
		origin := &url.URL{Scheme: "https", Host: domain, Path: "/"}
		jar.SetCookies(origin, []*http.Cookie{{
			Name:   name,
			Value:  value,
			Domain: cookie.Domain,
			Path:   "/",
			Secure: true,
		}})
	}

	return &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 30 {
				return errors.New("VK .ru authorize превысил 30 redirect")
			}
			return nil
		},
	}, nil
}
