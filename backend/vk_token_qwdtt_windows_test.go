//go:build windows

package backend

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestQWDTTLegacyVKCookieURLs(t *testing.T) {
	want := []string{
		"https://vk.com",
		"https://vk.ru",
		"https://m.vk.com",
		"https://m.vk.ru",
		"https://login.vk.com",
		"https://login.vk.ru",
		"https://oauth.vk.com",
		"https://oauth.vk.ru",
		"https://id.vk.com",
		"https://id.vk.ru",
	}
	if !reflect.DeepEqual(qwdttVKCookieURLs, want) {
		t.Fatalf("cookie URLs = %#v, want %#v", qwdttVKCookieURLs, want)
	}
}

func TestQWDTTLegacyOAuthRequestShape(t *testing.T) {
	req, err := newQWDTTLegacyOAuthRequest(
		context.Background(),
		buildLegacyVKAuthorizeURL(),
		"remixsid=session-value; remixlang=0",
		vkLegacyMobileUserAgent,
	)
	if err != nil {
		t.Fatalf("newQWDTTLegacyOAuthRequest() error = %v", err)
	}
	if req.Method != "GET" {
		t.Fatalf("method = %q, want GET", req.Method)
	}
	if got := req.Header.Get("Cookie"); got != "remixsid=session-value; remixlang=0" {
		t.Fatalf("Cookie = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != vkLegacyMobileUserAgent {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := req.Header.Get("Accept"); got != "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8" {
		t.Fatalf("Accept = %q", got)
	}
}

func TestMergeLegacyOAuthCookieHeaderCarriesFreshRedirectCookies(t *testing.T) {
	got := mergeLegacyOAuthCookieHeader(
		"remixsid=old; remixlang=0; keep=value",
		[]*http.Cookie{
			{Name: "remixsid", Value: "fresh"},
			{Name: "oauth_state", Value: "next"},
		},
	)
	want := "remixsid=fresh; oauth_state=next; remixlang=0; keep=value"
	if got != want {
		t.Fatalf("cookie header = %q, want %q", got, want)
	}
}
