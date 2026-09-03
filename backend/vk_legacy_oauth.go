package backend

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	vkLegacyClientID     = "6287487"
	vkLegacyRedirectURI  = "https://oauth.vk.com/blank.html"
	vkLegacyAuthorizeURL = "https://oauth.vk.com/authorize"
	vkLegacyOAuthState   = "wdtt"
)

type vkLegacyToken struct {
	AccessToken string
	ExpiresIn   time.Duration
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
