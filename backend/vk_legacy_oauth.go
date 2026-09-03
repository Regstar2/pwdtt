package backend

import (
	"errors"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	vkLegacyClientID         = "6287487"
	vkLegacyRedirectURI      = "https://oauth.vk.com/blank.html"
	vkLegacyAuthorizeURL     = "https://oauth.vk.com/authorize"
	vkLegacyOAuthState       = "wdtt"
	vkLegacyDesktopUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	vkLegacyLocationHrefRE = regexp.MustCompile(`(?i)location\.href\s*=\s*["']([^"']+)["']`)
	vkLegacyGrantURLRE     = regexp.MustCompile(`(?i)(https://login\.vk\.com/\?act=grant_access[^"'\\s<]+)`)
)

type vkLegacyToken struct {
	AccessToken string
	ExpiresIn   time.Duration
}

type vkLegacyAuthorizeHop struct {
	Token   vkLegacyToken
	NextURL string
	Err     error
}

func legacyVKLoginStartURL(attempt int) string {
	switch attempt {
	case 0:
		return "https://m.vk.ru/login"
	case 1:
		return "https://m.vk.ru/"
	default:
		return "https://vk.ru/login"
	}
}

func isLegacyVKIDLoginFlow(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return (strings.Contains(value, "id.vk.com") || strings.Contains(value, "id.vk.ru")) &&
		(strings.Contains(value, "authorize") || strings.Contains(value, "login") || strings.Contains(value, "auth"))
}

func buildLegacyVKAuthorizeURL() string {
	query := url.Values{
		"client_id":     {vkLegacyClientID},
		"display":       {"mobile"},
		"redirect_uri":  {vkLegacyRedirectURI},
		"response_type": {"token"},
		"scope":         {"messages"},
		"state":         {vkLegacyOAuthState},
		"v":             {vkAPIVersion},
	}
	return vkLegacyAuthorizeURL + "?" + query.Encode()
}

func parseLegacyVKTokenURL(raw string) (vkLegacyToken, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return vkLegacyToken{}, false, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return vkLegacyToken{}, false, nil
	}

	values := parsed.Query()
	if parsed.Fragment != "" {
		if fragmentValues, fragmentErr := url.ParseQuery(parsed.Fragment); fragmentErr == nil {
			for key, items := range fragmentValues {
				for _, item := range items {
					values.Add(key, item)
				}
		}
	}

	if oauthError := strings.TrimSpace(values.Get("error")); oauthError != "" {
		description := strings.TrimSpace(values.Get("error_description"))
		if description != "" {
			return vkLegacyToken{}, true, errors.New("авторизация VK отклонена: " + description)
		}
		return vkLegacyToken{}, true, errors.New("авторизация VK отклонена: " + oauthError)
	}

	accessToken := strings.TrimSpace(values.Get("access_token"))
	if accessToken == "" {
		return vkLegacyToken{}, false, nil
	}

	if state := strings.TrimSpace(values.Get("state")); state != "" && state != vkLegacyOAuthState {
		return vkLegacyToken{}, true, errors.New("VK OAuth вернул ответ с неверным state")
	}

	var expiresIn time.Duration
	if rawSeconds := strings.TrimSpace(values.Get("expires_in")); rawSeconds != "" {
		if seconds, parseErr := strconv.ParseInt(rawSeconds, 10, 64); parseErr == nil && seconds > 0 {
			expiresIn = time.Duration(seconds) * time.Second
		}
	}

	return vkLegacyToken{AccessToken: accessToken, ExpiresIn: expiresIn}, true, nil
}

func parseLegacyVKAuthorizeHop(currentURL string, statusCode int, location, body string) vkLegacyAuthorizeHop {
	location = strings.TrimSpace(html.UnescapeString(location))
	if location != "" {
		resolved := resolveLegacyVKRedirect(currentURL, location)
		if token, terminal, err := parseLegacyVKTokenURL(resolved); terminal {
			return vkLegacyAuthorizeHop{Token: token, Err: err}
		}
		return vkLegacyAuthorizeHop{NextURL: resolved}
	}

	if statusCode < 200 || statusCode >= 300 {
		return vkLegacyAuthorizeHop{}
	}

	if match := vkLegacyLocationHrefRE.FindStringSubmatch(body); len(match) > 1 {
		candidate := resolveLegacyVKRedirect(currentURL, html.UnescapeString(match[1]))
		if token, terminal, err := parseLegacyVKTokenURL(candidate); terminal {
			return vkLegacyAuthorizeHop{Token: token, Err: err}
		}
	}

	if match := vkLegacyGrantURLRE.FindStringSubmatch(body); len(match) > 1 {
		return vkLegacyAuthorizeHop{NextURL: html.UnescapeString(match[1])}
	}

	return vkLegacyAuthorizeHop{}
}

func resolveLegacyVKRedirect(currentURL, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}
	parsedNext, err := url.Parse(next)
	if err != nil {
		return next
	}
	if parsedNext.IsAbs() {
		return parsedNext.String()
	}
	base, err := url.Parse(strings.TrimSpace(currentURL))
	if err != nil {
		return next
	}
	return base.ResolveReference(parsedNext).String()
}
