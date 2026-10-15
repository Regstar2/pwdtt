package core

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCaptchaV2KnownWindowInitFixture(t *testing.T) {
	html, err := os.ReadFile("testdata/captcha_v2_known.html")
	if err != nil {
		t.Fatal(err)
	}
	page, err := parseCaptchaV2Page(string(html))
	if err != nil {
		t.Fatalf("known markup must parse: %v", err)
	}
	if page.Init == nil || page.Init.Data.ShowCaptchaType != "slider" ||
		len(page.Init.Data.CaptchaSettings) != 1 ||
		page.PowInput != "fixture-pow-input" || page.PowDifficulty != 3 ||
		!strings.Contains(page.ScriptURL, "not_robot_captcha.js") {
		t.Fatalf("unexpected parsed captcha page: %+v", page)
	}
}

func TestCaptchaV2PageFailureClassification(t *testing.T) {
	html, err := os.ReadFile("testdata/captcha_v2_unsupported.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, html, kind, reason string
	}{
		{"missing init", string(html), "unsupported_page", "init_missing"},
		{"changed schema", `<script>window.init = {"other":true};</script>`, "unsupported_page", "init_schema"},
		{"invalid json", `<script>window.init = {"data": malformed};</script>`, "parse", "init_invalid_json"},
		{"script absent", `<script>window.init = {"data":{"show_captcha_type":"checkbox"}};</script>`, "unsupported_page", "script_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCaptchaV2Page(tc.html)
			var failure *captchaV2Failure
			if !errors.As(err, &failure) || failure.Kind != tc.kind || failure.Reason != tc.reason {
				t.Fatalf("failure=%v; want %s/%s", err, tc.kind, tc.reason)
			}
			if !captchaV2Terminal(err) {
				t.Fatalf("systematic parse error must not be retried: %v", err)
			}
		})
	}
}

func TestCaptchaV2SafeHTTPDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		kind, mime string
		terminal bool
	}{
		{"network", "other", false},
		{"unsupported_page", "text/html", true},
		{"parse", "application/json", true},
		{"http_status", "application/json", true},
	} {
		err := &captchaV2Failure{
			Phase: "fetch_page", Kind: tc.kind, Reason: "test",
			Status: 503, ContentType: tc.mime, Redirected: true,
			Bytes: 200, DurationMs: 42,
		}
		if captchaV2Terminal(err) != tc.terminal {
			t.Fatalf("terminal(%s)=%v", tc.kind, captchaV2Terminal(err))
		}
		message := err.Error()
		for _, field := range []string{"phase=fetch_page", "http_status=503", "redirected=true", "bytes=200", "duration_ms=42"} {
			if !strings.Contains(message, field) {
				t.Fatalf("missing diagnostic %q from %q", field, message)
			}
		}
		if strings.Contains(message, "session_token") || strings.Contains(message, "https://") {
			t.Fatalf("leaked private request data: %s", message)
		}
	}
	if got := captchaV2MediaType("text/html; charset=utf-8"); got != "text/html" {
		t.Fatalf("media type=%q", got)
	}
	if got := captchaV2MediaType("text/html; secret=do-not-print; invalid-param"); got != "other" {
		t.Fatalf("unrecognized media type must be suppressed: %q", got)
	}
}

func TestCaptchaCoordinatorRejectsLateAndWrongResponses(t *testing.T) {
	b := newCaptchaCoordinator()
	r, err := b.begin(context.Background(), "https://id.vk.com/path?session_token=private", "manual", time.Minute)
	if err != nil { t.Fatal(err) }
	if b.submit("other-operation", "stale") {
		t.Fatal("accepted response for a different request")
	}
	if !b.submit(r.id, "valid") {
		t.Fatal("current request must accept response")
	}
	if got := <-r.result; got != "valid" {
		t.Fatalf("unexpected result %s", got)
	}
	if _, _, ok := b.active(r.id); !ok {
		t.Fatal("first browser open must be allowed")
	}
	if _, _, ok := b.active(r.id); ok {
		t.Fatal("must reject a duplicate browser")
	}
	b.launchFailed(r.id)
	if _, _, ok := b.active(r.id); !ok {
		t.Fatal("failed Edge launch must be retryable")
	}
	b.finish(r)
	if b.submit(r.id, "too late") {
		t.Fatal("accepted stale result after request finished")
	}
	next, err := b.begin(context.Background(), "https://id.vk.com/new", "manual", time.Minute)
	if err != nil { t.Fatal(err) }
	if b.submit(r.id, "late first request") {
		t.Fatal("accepted previous operation result")
	}
	b.finish(next)
}

func TestCaptchaCoordinatorCancelAndTimeout(t *testing.T) {
	b := newCaptchaCoordinator()
	r, err := b.begin(context.Background(), "https://id.vk.com/", "manual", time.Minute)
	if err != nil { t.Fatal(err) }
	if !b.cancel(r.id) { t.Fatal("expected cancellation") }
	select {
	case <-r.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancellation did not wake a waiting browser")
	}
	if b.submit(r.id, "late") {
		t.Fatal("canceled request accepted token")
	}
	b.finish(r)
	timeout, err := b.begin(context.Background(), "https://id.vk.com/", "manual", time.Millisecond)
	if err != nil { t.Fatal(err) }
	select {
	case <-timeout.ctx.Done():
		if !errors.Is(timeout.ctx.Err(), context.DeadlineExceeded) { t.Fatal(timeout.ctx.Err()) }
	case <-time.After(time.Second):
		t.Fatal("challenge deadline did not fire")
	}
	b.finish(timeout)
}
