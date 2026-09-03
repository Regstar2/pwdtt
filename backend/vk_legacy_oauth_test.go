package backend

import (
	"net/url"
	"testing"
	"time"
)

func TestBuildLegacyVKAuthorizeURLMatchesQWDTTFlow(t *testing.T) {
	parsed, err := url.Parse(buildLegacyVKAuthorizeURL())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "oauth.vk.com" || parsed.Path != "/authorize" {
		t.Fatalf("unexpected authorize URL: %s", parsed.String())
	}
	query := parsed.Query()
	want := map[string]string{
		"client_id":     vkLegacyClientID,
		"display":       "mobile",
		"redirect_uri":  vkLegacyRedirectURI,
		"response_type": "token",
		"scope":         "messages",
		"state":         vkLegacyOAuthState,
		"v":             "5.199",
	}
	for key, expected := range want {
		if got := query.Get(key); got != expected {
			t.Fatalf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestParseLegacyVKTokenURL(t *testing.T) {
	result, terminal, err := parseLegacyVKTokenURL(
		"https://oauth.vk.com/blank.html#access_token=secret-token&expires_in=86400&state=wdtt&user_id=1",
	)
	if err != nil {
		t.Fatalf("parseLegacyVKTokenURL() error = %v", err)
	}
	if !terminal {
		t.Fatal("expected terminal OAuth response")
	}
	if result.AccessToken != "secret-token" {
		t.Fatalf("token = %q", result.AccessToken)
	}
	if result.ExpiresIn != 24*time.Hour {
		t.Fatalf("expires = %s", result.ExpiresIn)
	}
}

func TestParseLegacyVKTokenURLIgnoresOrdinaryPages(t *testing.T) {
	_, terminal, err := parseLegacyVKTokenURL("https://id.vk.com/auth?act=login")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if terminal {
		t.Fatal("ordinary login page must not be terminal")
	}
}

func TestParseLegacyVKTokenURLRejectsWrongState(t *testing.T) {
	_, terminal, err := parseLegacyVKTokenURL(
		"https://oauth.vk.com/blank.html#access_token=secret-token&state=other",
	)
	if !terminal || err == nil {
		t.Fatalf("terminal=%v err=%v", terminal, err)
	}
}

func TestParseLegacyVKTokenURLError(t *testing.T) {
	_, terminal, err := parseLegacyVKTokenURL(
		"https://oauth.vk.com/blank.html#error=access_denied&error_description=Denied&state=wdtt",
	)
	if !terminal || err == nil {
		t.Fatalf("terminal=%v err=%v", terminal, err)
	}
}
