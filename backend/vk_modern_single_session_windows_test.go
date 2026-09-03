//go:build windows

package backend

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type vkModernSingleSessionTransport struct {
	t    *testing.T
	step int
}

func (tr *vkModernSingleSessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.step++

	switch tr.step {
	case 1:
		if req.Method != http.MethodGet || req.URL.Hostname() != "oauth.vk.ru" || req.URL.Path != "/authorize" {
			tr.fail(req, "unexpected authorize request")
		}
		if got := req.URL.Query().Get("scope"); got != vkMessagesScopeMask {
			tr.fail(req, "authorize scope = "+got)
		}
		return testVKHTTPResponse(req, http.StatusOK,
			`<script>window.init = {"data":{"hash":{"return_auth":"RETURN_HASH"}}};</script>`), nil

	case 2:
		if req.Method != http.MethodPost || req.URL.Hostname() != "login.vk.ru" || req.URL.Query().Get("act") != "connect_internal" {
			tr.fail(req, "unexpected connect_internal request")
		}
		form := readVKTestForm(tr.t, req)
		if got := form.Get("return_auth_hash"); got != "RETURN_HASH" {
			tr.fail(req, "return_auth_hash = "+got)
		}
		return testVKHTTPResponse(req, http.StatusOK,
			`{"type":"okay","data":{"access_token":"TEMP_TOKEN","auth_user_hash":"USER_HASH"}}`), nil

	case 3:
		if req.Method != http.MethodPost || req.URL.Hostname() != "api.vk.ru" || req.URL.Path != "/method/auth.getOauthToken" {
			tr.fail(req, "unexpected auth.getOauthToken request")
		}
		form := readVKTestForm(tr.t, req)
		checks := map[string]string{
			"hash":           "RETURN_HASH",
			"auth_user_hash": "USER_HASH",
			"access_token":   "TEMP_TOKEN",
			"scope":          vkMessagesScopeMask,
			"client_id":      vkLegacyClientID,
			"app_id":         vkLegacyClientID,
		}
		for key, want := range checks {
			if got := form.Get(key); got != want {
				tr.fail(req, key+" = "+got+", want "+want)
			}
		}
		return testVKHTTPResponse(req, http.StatusOK,
			`{"response":{"access_token":"FINAL_TOKEN","expires_in":3600}}`), nil
	default:
		tr.t.Fatalf("unexpected request step %d: %s %s", tr.step, req.Method, req.URL.String())
		return nil, nil
	}
}

func (tr *vkModernSingleSessionTransport) fail(req *http.Request, message string) {
	tr.t.Helper()
	tr.t.Fatalf("%s: step=%d %s %s", message, tr.step, req.Method, req.URL.String())
}

func testVKHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func readVKTestForm(t *testing.T, req *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestObtainModernRUTokenUsesOneHTTPSequence(t *testing.T) {
	transport := &vkModernSingleSessionTransport{t: t}
	client := &http.Client{Transport: transport}

	token, err := obtainModernRUTokenWithClient(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "FINAL_TOKEN" {
		t.Fatalf("access token = %q", token.AccessToken)
	}
	if transport.step != 3 {
		t.Fatalf("request count = %d, want 3", transport.step)
	}
}
