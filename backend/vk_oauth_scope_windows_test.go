//go:build windows

package backend

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type vkScopeCaptureTransport struct {
	body string
}

func (t *vkScopeCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		t.body = string(body)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"response":{}}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestVKGetOAuthTokenScopeUsesIntegerMask(t *testing.T) {
	capture := &vkScopeCaptureTransport{}
	transport := &vkOAuthScopeTransport{base: capture}

	form := url.Values{
		"scope":        {"messages"},
		"client_id":    {vkLegacyClientID},
		"access_token": {"temporary-token"},
	}
	req, err := http.NewRequest(http.MethodPost, vkModernRUGetOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatal(err)
	}

	values, err := url.ParseQuery(capture.body)
	if err != nil {
		t.Fatal(err)
	}
	if got := values.Get("scope"); got != vkMessagesScopeMask {
		t.Fatalf("scope = %q, want %q", got, vkMessagesScopeMask)
	}
	if got := values.Get("client_id"); got != vkLegacyClientID {
		t.Fatalf("client_id changed: %q", got)
	}
	if got := values.Get("access_token"); got != "temporary-token" {
		t.Fatalf("access_token changed: %q", got)
	}
}

func TestVKScopeTransportDoesNotTouchOtherRequests(t *testing.T) {
	capture := &vkScopeCaptureTransport{}
	transport := &vkOAuthScopeTransport{base: capture}

	body := "scope=messages&foo=bar"
	req, err := http.NewRequest(http.MethodPost, "https://api.vk.ru/method/calls.start", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if capture.body != body {
		t.Fatalf("non-OAuth body changed: %q", capture.body)
	}
}
