package core

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type vkTimeoutError struct{}

func (vkTimeoutError) Error() string   { return "timeout" }
func (vkTimeoutError) Timeout() bool   { return true }
func (vkTimeoutError) Temporary() bool { return true }

var vkTestClients = []VKCredentials{
	{ClientID: "one", ClientSecret: "secret-1"},
	{ClientID: "two", ClientSecret: "secret-2"},
	{ClientID: "three", ClientSecret: "secret-3"},
	{ClientID: "four", ClientSecret: "secret-4"},
}

func TestVKAuthTransportClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err error
		want vkAuthErrorKind
	}{
		{"dns timeout", &net.DNSError{Err: "i/o timeout", Name: "api.vk.me", IsTimeout: true}, vkAuthDNSTimeout},
		{"dns not found", &net.DNSError{Err: "no such host", Name: "api.vk.me", IsNotFound: true}, vkAuthDNSLookup},
		{"tcp connect timeout", &net.OpError{Op: "dial", Net: "tcp", Err: vkTimeoutError{}}, vkAuthTCPTimeout},
		{"tls handshake failure", x509.UnknownAuthorityError{}, vkAuthTLSFailure},
		{"tls read timeout", &net.OpError{Op: "read", Net: "tcp", Err: vkTimeoutError{}}, vkAuthHTTPTimeout},
		{"http timeout", context.DeadlineExceeded, vkAuthHTTPTimeout},
		{"network unreachable", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network unavailable")}, vkAuthNetwork},
		{"http 429", &vkAuthHTTPError{StatusCode: 429}, vkAuthRateLimit},
		{"vk rate limit", &vkCallsVKAPIError{Code: 29}, vkAuthRateLimit},
		{"invalid credential", &vkAuthAPIError{Code: 28}, vkAuthInvalidClient},
		{"captcha", errVKCaptchaWait, vkAuthCaptcha},
		{"cancel", context.Canceled, vkAuthCanceled},
		{"call unavailable", &CallUnavailableError{Code: 954}, vkAuthCallUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyVKAuthError(fmt.Errorf("wrapped: %w", tc.err)); got != tc.want {
				t.Fatalf("error kind=%s, expected %s", got, tc.want)
			}
		})
	}
}

func TestVKCallsRateLimitResponseDoesNotFallback(t *testing.T) {
	apiErr := vkCallsAPIError(map[string]interface{}{
		"error": map[string]interface{}{"error_code": float64(29), "error_msg": "limit"},
	})
	if classifyVKAuthError(apiErr) != vkAuthRateLimit {
		t.Fatalf("rate limit classified as %s", classifyVKAuthError(apiErr))
	}
	wrapped := newVKCallsFailure("step1 auth.getAnonymToken", vkCallsAPIErrorKind(apiErr), apiErr)
	if vkAuthShouldUseLegacy(wrapped) {
		t.Fatal("VK API rate limit must not trigger legacy credentials fallback")
	}
}

func TestVKAuthDNSTimeoutDoesNotRotateClients(t *testing.T) {
	var calls int
	dns := &net.DNSError{Err: "i/o timeout", Name: "api.vk.me", IsTimeout: true}
	_, _, _, err := tryVKCredentialPairs(context.Background(), vkTestClients,
		func(ctx context.Context, cred VKCredentials, attempt int) (string, string, []string, error) {
			calls++
			return "", "", nil, newVKAuthTransportError(ctx, "anonymous_token", "api.vk.me", dns)
		})
	if classifyVKAuthError(err) != vkAuthDNSTimeout || calls != 1 {
		t.Fatalf("kind=%s attempts=%d; expected DNS timeout after one attempt", classifyVKAuthError(err), calls)
	}
	var original *net.DNSError
	if !errors.As(err, &original) {
		t.Fatalf("lost typed DNS cause: %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("sensitive URL or credentials in error: %v", err)
	}
	if msg := vkAuthUIFailureMessage(err); msg != "Не удалось разрешить адрес VK API / проблема DNS" {
		t.Fatalf("UI error=%q", msg)
	}
}

func TestVKAuthOnlyCredentialsRotate(t *testing.T) {
	for _, errCase := range []error{
		&vkAuthHTTPError{StatusCode: 429},
		&vkAuthAPIError{Code: 14},
		context.Canceled,
		&net.OpError{Op: "dial", Err: vkTimeoutError{}},
	} {
		var calls int
		_, _, _, err := tryVKCredentialPairs(context.Background(), vkTestClients,
			func(context.Context, VKCredentials, int) (string, string, []string, error) {
				calls++
				return "", "", nil, errCase
			})
		if calls != 1 || classifyVKAuthError(err) != classifyVKAuthError(errCase) {
			t.Fatalf("error=%v attempts=%d", errCase, calls)
		}
	}
}

func TestVKAuthCredentialFallbackSuccess(t *testing.T) {
	var ids []string
	u, p, addr, err := tryVKCredentialPairs(context.Background(), vkTestClients,
		func(_ context.Context, c VKCredentials, _ int) (string, string, []string, error) {
			ids = append(ids, c.ClientID)
			if c.ClientID == "one" {
				return "", "", nil, &vkAuthAPIError{Code: 28}
			}
			return "user", "pass", []string{"turn.example:443"}, nil
		})
	if err != nil || u != "user" || p != "pass" || len(addr) != 1 ||
		len(ids) != 2 || ids[0] != "one" || ids[1] != "two" {
		t.Fatalf("unexpected fallback result: ids=%v creds=%s/%s addrs=%v err=%v", ids, u, p, addr, err)
	}
}

func TestVKCallsLegacyFallbackPolicy(t *testing.T) {
	for _, tc := range []struct {
		err error
		want bool
	}{
		{newVKCallsFailure("step1", vkCallsFailureNetwork, &net.DNSError{IsTimeout: true}), false},
		{newVKCallsFailure("step1", vkCallsFailureNetwork, context.DeadlineExceeded), false},
		{newVKCallsFailure("step1", vkCallsFailureVKAPI, &vkAuthHTTPError{StatusCode: 429}), false},
		{newVKCallsFailure("step3", vkCallsFailureCaptcha, errVKCaptchaWait), false},
		{newVKCallsFailure("step2", vkCallsFailureCall, &CallUnavailableError{Code: 951}), false},
		{newVKCallsFailure("step1", vkCallsFailureVKAPI, &vkCallsVKAPIError{Code: 28}), true},
		{newVKCallsFailure("step1", vkCallsFailureDecode, errors.New("bad json")), true},
		{newVKCallsFailure("preflight", vkCallsFailureSkipped, errors.New("disabled")), true},
	} {
		if got := vkAuthShouldUseLegacy(tc.err); got != tc.want {
			t.Fatalf("error=%v fallback=%v want=%v", tc.err, got, tc.want)
		}
	}
}

func TestVKAuthCancellationStopsAttemptsAndWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, _, _, err := tryVKCredentialPairs(ctx, vkTestClients,
			func(ctx context.Context, _ VKCredentials, _ int) (string, string, []string, error) {
				calls.Add(1)
				<-ctx.Done()
				return "", "", nil, ctx.Err()
			})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || calls.Load() > 1 {
			t.Fatalf("err=%v calls=%d", err, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop credentials attempts")
	}
}

func TestVKAuthCancelWhileWaitingForCacheRefresh(t *testing.T) {
	cache := getStreamCache(987654321)
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, _, err := getVkCredsCached(ctx, "test-hash", 987654321)
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wanted canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cache mutex wait ignored cancellation")
	}
}

func TestVKAuthUnspecifiedTransportFailureNeverRotates(t *testing.T) {
	transport := newVKAuthTransportError(context.Background(), "step1", "api.vk.me", errors.New("untyped adapter error"))
	if classifyVKAuthError(transport) != vkAuthNetwork {
		t.Fatalf("untyped request transport error misclassified: %v", transport)
	}
	if vkAuthShouldUseLegacy(newVKCallsFailure("step1", vkCallsFailureNetwork, transport)) {
		t.Fatal("network transport failure must not fall back to client_id rotation")
	}
}

func TestVKAuthDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, _, err := tryVKCredentialPairs(ctx, vkTestClients,
		func(ctx context.Context, _ VKCredentials, _ int) (string, string, []string, error) {
			<-ctx.Done()
			return "", "", nil, ctx.Err()
		})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("deadline ignored: %v", err)
	}
}
