//go:build windows

package backend

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

const vkMessagesScopeMask = "4096"

type vkOAuthScopeTransport struct {
	base http.RoundTripper
}

func init() {
	base := http.DefaultTransport
	http.DefaultTransport = &vkOAuthScopeTransport{base: base}
}

func (t *vkOAuthScopeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	if !isModernVKGetOAuthTokenRequest(req) || req.Body == nil {
		return base.RoundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	values, err := url.ParseQuery(string(body))
	if err != nil {
		clone := req.Clone(req.Context())
		clone.Header = req.Header.Clone()
		clone.Body = io.NopCloser(strings.NewReader(string(body)))
		clone.ContentLength = int64(len(body))
		return base.RoundTrip(clone)
	}

	if values.Get("scope") == "messages" {
		values.Set("scope", vkMessagesScopeMask)
	}
	encoded := values.Encode()

	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Body = io.NopCloser(strings.NewReader(encoded))
	clone.ContentLength = int64(len(encoded))
	clone.Header.Del("Content-Length")
	return base.RoundTrip(clone)
}

func isModernVKGetOAuthTokenRequest(req *http.Request) bool {
	if req == nil || req.URL == nil || req.Method != http.MethodPost {
		return false
	}
	return strings.EqualFold(req.URL.Hostname(), "api.vk.ru") &&
		req.URL.Path == "/method/auth.getOauthToken"
}
