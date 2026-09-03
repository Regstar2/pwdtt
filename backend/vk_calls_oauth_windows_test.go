//go:build windows

package backend

import (
	"net/url"
	"testing"
)

func TestVKCallsOAuthHTTPAuthorizeMatchesAutoAPIFlow(t *testing.T) {
	parsed, err := url.Parse(buildVKCallsHTTPAuthorizeURL())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "oauth.vk.com" || parsed.Path != "/authorize" {
		t.Fatalf("unexpected HTTP authorize URL: %s", parsed.String())
	}
	q := parsed.Query()
	checks := map[string]string{
		"client_id":     "7793118",
		"display":       "mobile",
		"redirect_uri":  "https://oauth.vk.ru/blank.html",
		"response_type": "token",
		"scope":         "1073737727",
		"revoke":        "1",
		"v":             "5.199",
	}
	for key, want := range checks {
		if got := q.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestVKCallsOAuthBrowserAuthorizeMatchesAutoAPIFlow(t *testing.T) {
	parsed, err := url.Parse(buildVKCallsBrowserAuthorizeURL())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "oauth.vk.ru" || parsed.Path != "/authorize" {
		t.Fatalf("unexpected browser authorize URL: %s", parsed.String())
	}
	q := parsed.Query()
	if q.Get("client_id") != vkCallsOAuthClientID {
		t.Fatalf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("scope") != vkCallsOAuthScope {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
	if q.Get("redirect_uri") != vkCallsOAuthRedirectURI {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "token" || q.Get("revoke") != "1" {
		t.Fatalf("unexpected browser OAuth parameters: %s", parsed.RawQuery)
	}
}

func TestVKCallsOAuthUsesCallsApp(t *testing.T) {
	if vkCallsOAuthClientID != "7793118" {
		t.Fatalf("VK Calls app id = %q", vkCallsOAuthClientID)
	}
}
