package backend

import (
	"net/url"
	"testing"
	"time"
)

func TestLegacyVKLoginStartURLsMatchQWDTTFallbacks(t *testing.T) {
	want := []string{
		"https://m.vk.ru/login",
		"https://m.vk.ru/",
		"https://vk.ru/login",
	}
	for attempt, expected := range want {
		if got := legacyVKLoginStartURL(attempt); got != expected {
			t.Fatalf("attempt %d: %q, want %q", attempt, got, expected)
		}
	}
	if got := legacyVKLoginStartURL(99); got != want[2] {
		t.Fatalf("fallback attempt: %q, want %q", got, want[2])
	}
}

func TestIsLegacyVKIDLoginFlow(t *testing.T) {
	for _, raw := range []string{
		"https://id.vk.ru/authorize?foo=bar",
		"https://id.vk.com/auth?act=login",
		"https://id.vk.ru/login",
	} {
		if !isLegacyVKIDLoginFlow(raw) {
			t.Fatalf("expected login flow for %q", raw)
		}
	}
	for _, raw := range []string{
		"https://m.vk.ru/",
		"https://vk.ru/feed",
		"https://oauth.vk.com/blank.html#access_token=x",
	} {
		if isLegacyVKIDLoginFlow(raw) {
			t.Fatalf("did not expect login flow for %q", raw)
		}
	}
}

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

func TestParseLegacyVKAuthorizeHopFollowsLocation(t *testing.T) {
	hop := parseLegacyVKAuthorizeHop(
		"https://oauth.vk.com/authorize?client_id=6287487",
		302,
		"https://login.vk.com/?act=grant_access&client_id=6287487&amp;foo=bar",
		"",
	)
	if hop.Err != nil {
		t.Fatalf("unexpected error: %v", hop.Err)
	}
	if hop.NextURL != "https://login.vk.com/?act=grant_access&client_id=6287487&foo=bar" {
		t.Fatalf("next URL = %q", hop.NextURL)
	}
}

func TestParseLegacyVKAuthorizeHopExtractsTokenFromLocation(t *testing.T) {
	hop := parseLegacyVKAuthorizeHop(
		"https://login.vk.com/?act=grant_access",
		302,
		"https://oauth.vk.com/blank.html#access_token=secret-token&expires_in=3600&state=wdtt",
		"",
	)
	if hop.Err != nil {
		t.Fatalf("unexpected error: %v", hop.Err)
	}
	if hop.Token.AccessToken != "secret-token" || hop.Token.ExpiresIn != time.Hour {
		t.Fatalf("unexpected token: %#v", hop.Token)
	}
}

func TestParseLegacyVKAuthorizeHopExtractsTokenFromLocationHref(t *testing.T) {
	hop := parseLegacyVKAuthorizeHop(
		"https://oauth.vk.com/authorize",
		200,
		"",
		`<script>location.href='https://oauth.vk.com/blank.html#access_token=secret-token&state=wdtt'</script>`,
	)
	if hop.Err != nil {
		t.Fatalf("unexpected error: %v", hop.Err)
	}
	if hop.Token.AccessToken != "secret-token" {
		t.Fatalf("token = %q", hop.Token.AccessToken)
	}
}

func TestParseLegacyVKAuthorizeHopFindsGrantAccess(t *testing.T) {
	hop := parseLegacyVKAuthorizeHop(
		"https://oauth.vk.com/authorize",
		200,
		"",
		`<a href="https://login.vk.com/?act=grant_access&amp;client_id=6287487&amp;hash=abc">continue</a>`,
	)
	if hop.Err != nil {
		t.Fatalf("unexpected error: %v", hop.Err)
	}
	if hop.NextURL != "https://login.vk.com/?act=grant_access&client_id=6287487&hash=abc" {
		t.Fatalf("next URL = %q", hop.NextURL)
	}
}

func TestParseLegacyVKAuthorizeHopStopsOnHTTP405(t *testing.T) {
	hop := parseLegacyVKAuthorizeHop("https://oauth.vk.com/authorize", 405, "", "Method Not Allowed")
	if hop.Err != nil || hop.Token.AccessToken != "" || hop.NextURL != "" {
		t.Fatalf("unexpected hop: %#v", hop)
	}
}
