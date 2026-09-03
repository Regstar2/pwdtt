//go:build windows

package backend

import (
	"net/url"
	"testing"
)

func TestBuildModernRUAuthorizeURL(t *testing.T) {
	parsed, err := url.Parse(buildModernRUAuthorizeURL())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "oauth.vk.ru" || parsed.Path != "/authorize" {
		t.Fatalf("unexpected URL: %s", parsed.String())
	}
	query := parsed.Query()
	if query.Get("client_id") != "6287487" {
		t.Fatalf("client_id = %q", query.Get("client_id"))
	}
	if query.Get("scope") != "messages" {
		t.Fatalf("scope = %q", query.Get("scope"))
	}
	if query.Get("response_type") != "token" {
		t.Fatalf("response_type = %q", query.Get("response_type"))
	}
}

func TestExtractModernRUReturnAuthHashFromWindowInit(t *testing.T) {
	body := `<script>window.init = {"data":{"hash":{"return_auth":"abc123HASH"}}};</script>`
	if got := extractModernRUReturnAuthHash(body); got != "abc123HASH" {
		t.Fatalf("return_auth = %q", got)
	}
}

func TestExtractModernRUReturnAuthHashFromURL(t *testing.T) {
	raw := "https://oauth.vk.ru/authorize?redirect_uri=1&return_auth_hash=urlHash123"
	if got := extractModernRUReturnAuthHashFromURL(raw); got != "urlHash123" {
		t.Fatalf("return_auth_hash = %q", got)
	}
}

func TestParseModernRUAccessTokenURLDirect(t *testing.T) {
	raw := "https://oauth.vk.ru/blank.html#access_token=secret-token&expires_in=3600"
	token, ok := parseModernRUAccessTokenURL(raw)
	if !ok || token.AccessToken != "secret-token" {
		t.Fatalf("token=%#v ok=%v", token, ok)
	}
}

func TestParseModernRUAccessTokenURLAuthorizeURL(t *testing.T) {
	nested := "https://oauth.vk.ru/blank.html#access_token=nested-token&expires_in=3600"
	raw := "https://oauth.vk.ru/authorize?access_token=1&authorize_url=" + url.QueryEscape(nested)
	token, ok := parseModernRUAccessTokenURL(raw)
	if !ok || token.AccessToken != "nested-token" {
		t.Fatalf("token=%#v ok=%v", token, ok)
	}
}
