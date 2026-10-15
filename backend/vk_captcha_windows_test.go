//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"testing"
)

func TestVKCaptchaCheckURLAllowList(t *testing.T) {
	for _, tc := range []struct {
		url string
		want bool
	}{
		{"https://api.vk.ru/method/captchaNotRobot.check?v=5.131", true},
		{"https://api.vk.com/method/captchaNotRobot.check", true},
		{"http://api.vk.ru/method/captchaNotRobot.check", false},
		{"https://api.vk.ru.evil.example/method/captchaNotRobot.check", false},
		{"https://evil.example/method/captchaNotRobot.check", false},
		{"https://id.vk.com/method/captchaNotRobot.settings", false},
	} {
		if got := isCaptchaCheckEndpoint(tc.url); got != tc.want {
			t.Fatalf("captcha endpoint %q: got %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestVKCaptchaSuccessTokenParsing(t *testing.T) {
	for _, tc := range []struct {
		body, want string
	}{
		{`{"response":{"status":"ok","success_token":"synthetic-token"}}`, "synthetic-token"},
		{`{"response":{"status":"bot","success_token":"should-not-use"}}`, ""},
		{`{"response":{"status":"ok"}}`, ""},
		{`{"error":{"error_code":14}}`, ""},
		{`not-json`, ""},
	} {
		if got := captchaTokenFromVKResponse([]byte(tc.body)); got != tc.want {
			t.Fatalf("unexpected captcha result for %q", tc.body)
		}
	}
}

func TestVKCaptchaCDPEventParsing(t *testing.T) {
	payload := []byte(`{"method":"Network.responseReceived","params":{"requestId":"test-42","response":{"url":"https://api.vk.ru/method/captchaNotRobot.check?v=5.131"}}}`)
	var ev captchaCDPEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Params.RequestID != "test-42" || !isCaptchaCheckEndpoint(ev.Params.Response.URL) {
		t.Fatalf("did not recognize known API response: %+v", ev)
	}
}

func TestVKCaptchaBrowserRejectsUntrustedHost(t *testing.T) {
	if _, err := solveVKCaptchaInBrowser(context.Background(), "https://id.vk.com.evil.example/path"); err == nil {
		t.Fatal("untrusted CAPTCHA URL must not launch a browser")
	}
}
