package core

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func vkCallsCaptchaFixture() map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"error_code":      float64(14),
			"redirect_uri":    "https://id.vk.com/captcha?session_token=private-session",
			"captcha_sid":     "private-sid",
			"captcha_ts":      float64(42),
			"captcha_attempt": float64(0),
		},
	}
}

func TestVKCallsCaptchaResumesEachAPIStep(t *testing.T) {
	for _, step := range []string{
		"step1 auth.getAnonymToken",
		"step2 messages.getCallPreview",
		"step3 messages.getAnonymCallToken",
	} {
		t.Run(step, func(t *testing.T) {
			const original = "https://api.vk.me/method/example?v=5.276&device_id=stable-id&link=a%2Fb"
			requests, solves := 0, 0
			request := func(gotStep, endpoint string) (map[string]interface{}, error) {
				requests++
				if gotStep != step {
					t.Fatalf("step = %q, want %q", gotStep, step)
				}
				if requests == 1 {
					if endpoint != original {
						t.Fatalf("original endpoint changed")
					}
					return vkCallsCaptchaFixture(), nil
				}
				if requests != 2 {
					t.Fatalf("unexpected request %d", requests)
				}
				if !strings.HasPrefix(endpoint, original+"&") {
					t.Fatalf("retry changed the original request")
				}
				parsed, err := url.Parse(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				values := parsed.Query()
				for key, want := range map[string]string{
					"device_id": "stable-id",
					"link": "a/b",
					"captcha_sid": "private-sid",
					"success_token": "private-success",
					"captcha_ts": "42",
					"captcha_attempt": "1",
					"captcha_key": "",
					"is_sound_captcha": "0",
				} {
					if got := values.Get(key); got != want {
						t.Errorf("%s: got %q, want %q", key, got, want)
					}
				}
				if _, ok := values["captcha_key"]; !ok {
					t.Error("missing captcha_key")
				}
				return map[string]interface{}{"response": map[string]interface{}{"token": "resumed"}}, nil
			}
			solve := func(ctx context.Context, id int, challenge *VkCaptchaError) (string, error) {
				solves++
				if id != 100 || challenge.SessionToken != "private-session" || challenge.CaptchaSid != "private-sid" {
					t.Fatal("wrong challenge identity")
				}
				return "private-success", nil
			}
			result, err := vkCallsRequestWithCaptcha(context.Background(), 100, step, original, request, solve)
			if err != nil {
				t.Fatalf("challenge failed: %v", err)
			}
			if result["response"] == nil || requests != 2 || solves != 1 {
				t.Fatalf("resumption: response=%v requests=%d solves=%d", result, requests, solves)
			}
		})
	}
}

func TestVKCallsCaptchaDoesNotRetrySameRejectedChallenge(t *testing.T) {
	requests, solves := 0, 0
	_, err := vkCallsRequestWithCaptcha(context.Background(), 3, "step3", "https://api.vk.me/example?v=1",
		func(_, _ string) (map[string]interface{}, error) {
			requests++
			return vkCallsCaptchaFixture(), nil
		},
		func(_ context.Context, _ int, _ *VkCaptchaError) (string, error) {
			solves++
			return "private-success", nil
		})
	if classifyVKAuthError(err) != vkAuthCaptcha || requests != 2 || solves != 1 {
		t.Fatalf("unexpected retry behavior: err=%v requests=%d solves=%d", err, requests, solves)
	}
	if strings.Contains(err.Error(), "private-session") || strings.Contains(err.Error(), "private-success") {
		t.Fatalf("captcha secrets escaped into error: %v", err)
	}
}

func TestVKCallsCaptchaIncompleteChallengeDoesNotPrompt(t *testing.T) {
	challenge := vkCallsCaptchaFixture()
	delete(challenge["error"].(map[string]interface{}), "captcha_sid")
	solves, requests := 0, 0
	_, err := vkCallsRequestWithCaptcha(context.Background(), 3, "step2", "https://api.vk.me/example?v=1",
		func(_, _ string) (map[string]interface{}, error) {
			requests++
			return challenge, nil
		},
		func(_ context.Context, _ int, _ *VkCaptchaError) (string, error) {
			solves++
			return "unused", nil
		})
	if classifyVKAuthError(err) != vkAuthCaptcha || requests != 1 || solves != 0 {
		t.Fatalf("incomplete captcha retried: err=%v requests=%d solves=%d", err, requests, solves)
	}
}

func TestVKCallsCaptchaCancellationStopsReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := 0
	_, err := vkCallsRequestWithCaptcha(ctx, 1, "step1", "https://api.vk.me/example?v=1",
		func(_, _ string) (map[string]interface{}, error) {
			requests++
			return vkCallsCaptchaFixture(), nil
		},
		func(_ context.Context, _ int, _ *VkCaptchaError) (string, error) {
			cancel()
			return "", context.Canceled
		})
	if !errors.Is(err, context.Canceled) || requests != 1 {
		t.Fatalf("canceled request replayed: err=%v requests=%d", err, requests)
	}
}

func TestVKCallsNonCaptchaErrorsDoNotPrompt(t *testing.T) {
	requests, solves := 0, 0
	_, err := vkCallsRequestWithCaptcha(context.Background(), 1, "step2", "https://api.vk.me/example?v=1",
		func(_, _ string) (map[string]interface{}, error) {
			requests++
			return map[string]interface{}{"error": map[string]interface{}{
				"error_code": float64(29),
				"error_msg": "Rate limited",
			}}, nil
		},
		func(_ context.Context, _ int, _ *VkCaptchaError) (string, error) {
			solves++
			return "", nil
		})
	if classifyVKAuthError(err) != vkAuthRateLimit || requests != 1 || solves != 0 {
		t.Fatalf("rate limit unexpectedly retried: err=%v requests=%d solves=%d", err, requests, solves)
	}
}
