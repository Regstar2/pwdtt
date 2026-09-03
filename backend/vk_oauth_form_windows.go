//go:build windows

package backend

import "net/url"

const vkMessagesScopeMask = "4096"

func buildModernRUGetOAuthTokenForm(returnAuthHash, authUserHash, accessToken string) url.Values {
	return url.Values{
		"hash":             {returnAuthHash},
		"auth_user_hash":   {authUserHash},
		"app_id":           {vkLegacyClientID},
		"client_id":        {vkLegacyClientID},
		"scope":            {vkMessagesScopeMask},
		"access_token":     {accessToken},
		"is_seamless_auth": {"1"},
		"v":                {vkModernRUAPIVersion},
	}
}
