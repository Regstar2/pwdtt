//go:build windows

package backend

import (
	"strings"
	"testing"
)

func TestExtractModernRUReturnAuthHashFlexible(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "plain return_auth",
			body: `<script>window.init = {"data":{"hash":{"return_auth":"hash-one"}}};</script>`,
			want: "hash-one",
		},
		{
			name: "return_auth_hash",
			body: `<script>var x={"return_auth_hash":"hash-two"};</script>`,
			want: "hash-two",
		},
		{
			name: "html escaped",
			body: `&quot;return_auth&quot;:&quot;hash-three&quot;`,
			want: "hash-three",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractModernRUReturnAuthHashFlexible(tt.body); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindJSONStringByKey(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"hash": map[string]any{
				"return_auth": "nested-hash",
			},
		},
	}
	if got := findJSONStringByKey(payload, "return_auth"); got != "nested-hash" {
		t.Fatalf("got %q", got)
	}
}

func TestSafeModernRUPageDiagDoesNotExposeQueryValues(t *testing.T) {
	diag := safeModernRUPageDiag(
		"https://oauth.vk.ru/authorize?client_id=6287487&access_token=SECRET&return_auth_hash=PRIVATE",
		`window.init = {"data":{"hash":{"return_auth":"BODY-SECRET"}}};`,
		true,
	)
	for _, secret := range []string{"SECRET", "PRIVATE", "BODY-SECRET", "6287487"} {
		if strings.Contains(diag, secret) {
			t.Fatalf("diagnostic leaked %q: %s", secret, diag)
		}
	}
	for _, marker := range []string{"host=oauth.vk.ru", "query_keys=", "p_cookie=true", "window_init=true", "return_auth_marker=true"} {
		if !strings.Contains(diag, marker) {
			t.Fatalf("diagnostic missing %q: %s", marker, diag)
		}
	}
}
