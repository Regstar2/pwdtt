//go:build windows

package backend

import "testing"

func TestBuildModernRUGetOAuthTokenFormUsesNumericScope(t *testing.T) {
	form := buildModernRUGetOAuthTokenForm("return-hash", "user-hash", "temporary-token")

	if got := form.Get("scope"); got != "4096" {
		t.Fatalf("scope = %q, want 4096", got)
	}
	if got := form.Get("hash"); got != "return-hash" {
		t.Fatalf("hash = %q", got)
	}
	if got := form.Get("auth_user_hash"); got != "user-hash" {
		t.Fatalf("auth_user_hash = %q", got)
	}
	if got := form.Get("access_token"); got != "temporary-token" {
		t.Fatalf("access_token = %q", got)
	}
	if got := form.Get("client_id"); got != vkLegacyClientID {
		t.Fatalf("client_id = %q", got)
	}
	if got := form.Get("app_id"); got != vkLegacyClientID {
		t.Fatalf("app_id = %q", got)
	}
	if got := form.Get("is_seamless_auth"); got != "1" {
		t.Fatalf("is_seamless_auth = %q", got)
	}
	if got := form.Get("v"); got != vkModernRUAPIVersion {
		t.Fatalf("v = %q", got)
	}
}
