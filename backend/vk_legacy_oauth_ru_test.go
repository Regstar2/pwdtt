package backend

import "testing"

func TestParseLegacyVKAuthorizeHopFindsGrantAccessOnVKRU(t *testing.T) {
	hop := parseLegacyVKAuthorizeHop(
		"https://oauth.vk.ru/authorize",
		200,
		"",
		`<a href="https://login.vk.ru/?act=grant_access&amp;client_id=6287487&amp;hash=abc">continue</a>`,
	)
	if hop.Err != nil {
		t.Fatalf("unexpected error: %v", hop.Err)
	}
	if hop.NextURL != "https://login.vk.ru/?act=grant_access&client_id=6287487&hash=abc" {
		t.Fatalf("next URL = %q", hop.NextURL)
	}
}
