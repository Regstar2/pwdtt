package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const vkCredentialsDeadline = 90 * time.Second

type vkAuthErrorKind string

const (
	vkAuthSuccess        vkAuthErrorKind = "success"
	vkAuthDNSLookup      vkAuthErrorKind = "dns_lookup_failure"
	vkAuthDNSTimeout     vkAuthErrorKind = "dns_lookup_timeout"
	vkAuthTCPTimeout     vkAuthErrorKind = "tcp_connect_timeout"
	vkAuthTLSFailure     vkAuthErrorKind = "tls_failure"
	vkAuthNetwork        vkAuthErrorKind = "network_unreachable"
	vkAuthHTTPTimeout    vkAuthErrorKind = "http_timeout"
	vkAuthRateLimit      vkAuthErrorKind = "rate_limit"
	vkAuthInvalidClient  vkAuthErrorKind = "credential_rejected"
	vkAuthCaptcha        vkAuthErrorKind = "captcha"
	vkAuthCanceled       vkAuthErrorKind = "canceled"
	vkAuthVKAPI          vkAuthErrorKind = "vk_api_error"
	vkAuthCallUnavailable vkAuthErrorKind = "call_unavailable"
	vkAuthOther          vkAuthErrorKind = "other"
)

var errVKCaptchaWait = errors.New("CAPTCHA_WAIT_REQUIRED")

// vkAuthTransportError retains the original cause for errors.Is/As, but its
// printable form deliberately omits full request URLs, hashes and tokens.
type vkAuthTransportError struct {
	Stage    string
	Endpoint string
	Kind     vkAuthErrorKind
	Err      error
}

func (e *vkAuthTransportError) Error() string {
	return fmt.Sprintf("VK transport failure stage=%s endpoint=%s kind=%s", e.Stage, e.Endpoint, e.Kind)
}

func (e *vkAuthTransportError) Unwrap() error { return e.Err }

func newVKAuthTransportError(ctx context.Context, stage, endpoint string, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	kind := classifyVKAuthError(err)
	if kind == vkAuthOther {
		// Do()/ReadAll failed at the transport layer, even if the TLS
		// adapter discarded the typed inner cause.
		kind = vkAuthNetwork
	}
	return &vkAuthTransportError{
		Stage: stage, Endpoint: endpoint, Kind: kind, Err: err,
	}
}

type vkAuthHTTPError struct{ StatusCode int }

func (e *vkAuthHTTPError) Error() string {
	return fmt.Sprintf("VK HTTP status=%d", e.StatusCode)
}

type vkAuthAPIError struct{ Code int }

func (e *vkAuthAPIError) Error() string {
	return fmt.Sprintf("VK API error_code=%d", e.Code)
}

func classifyVKAuthError(err error) vkAuthErrorKind {
	if err == nil {
		return vkAuthSuccess
	}
	if errors.Is(err, context.Canceled) {
		return vkAuthCanceled
	}
	var transport *vkAuthTransportError
	if errors.As(err, &transport) {
		return transport.Kind
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTimeout || dnsErr.Timeout() {
			return vkAuthDNSTimeout
		}
		return vkAuthDNSLookup
	}
	var unavailable *dnsUnavailableError
	if errors.As(err, &unavailable) {
		if unavailable.kind == "timeout" {
			return vkAuthDNSTimeout
		}
		return vkAuthDNSLookup
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return vkAuthHTTPTimeout
	}
	var httpErr *vkAuthHTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 429 {
			return vkAuthRateLimit
		}
		return vkAuthVKAPI
	}
	var apiErr *vkAuthAPIError
	if errors.As(err, &apiErr) {
		return vkAuthAPIErrorCodeKind(apiErr.Code)
	}
	var callsErr *vkCallsVKAPIError
	if errors.As(err, &callsErr) {
		return vkAuthAPIErrorCodeKind(callsErr.Code)
	}
	var captchaErr *VkCaptchaError
	if errors.As(err, &captchaErr) || errors.Is(err, errVKCaptchaWait) {
		return vkAuthCaptcha
	}
	var callErr *CallUnavailableError
	if errors.As(err, &callErr) {
		return vkAuthCallUnavailable
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var timeout net.Error
		if errors.As(opErr, &timeout) && timeout.Timeout() {
			if opErr.Op == "dial" {
				return vkAuthTCPTimeout
			}
			return vkAuthHTTPTimeout
		}
		return vkAuthNetwork
	}
	var certificateErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var recordErr *tls.RecordHeaderError
	if errors.As(err, &certificateErr) || errors.As(err, &hostnameErr) || errors.As(err, &recordErr) {
		return vkAuthTLSFailure
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ECONNREFUSED) {
		return vkAuthNetwork
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return vkAuthHTTPTimeout
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// A request transport error with an untyped cause is still not a
		// client credential rejection.
		return vkAuthNetwork
	}
	return vkAuthOther
}

func vkAuthAPIErrorCodeKind(code int) vkAuthErrorKind {
	switch code {
	case 5, 27, 28:
		return vkAuthInvalidClient
	case 14:
		return vkAuthCaptcha
	case 29:
		return vkAuthRateLimit
	default:
		return vkAuthVKAPI
	}
}

func vkAuthIsTransport(kind vkAuthErrorKind) bool {
	switch kind {
	case vkAuthDNSLookup, vkAuthDNSTimeout, vkAuthTCPTimeout, vkAuthTLSFailure,
		vkAuthNetwork, vkAuthHTTPTimeout:
		return true
	}
	return false
}

func vkAuthShouldUseLegacy(err error) bool {
	kind := classifyVKAuthError(err)
	if vkAuthIsTransport(kind) || kind == vkAuthCanceled ||
		kind == vkAuthCaptcha || kind == vkAuthRateLimit || kind == vkAuthCallUnavailable {
		return false
	}
	var failure *vkCallsFailure
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.Kind {
	case vkCallsFailureSkipped, vkCallsFailureSetup, vkCallsFailureDecode,
		vkCallsFailureVKAPI, vkCallsFailureOKCDN, vkCallsFailureParse:
		return true
	}
	return false
}

func vkAuthUIFailureMessage(err error) string {
	switch classifyVKAuthError(err) {
	case vkAuthDNSLookup, vkAuthDNSTimeout:
		return "Не удалось разрешить адрес VK API / проблема DNS"
	case vkAuthTCPTimeout, vkAuthTLSFailure, vkAuthNetwork, vkAuthHTTPTimeout:
		return "Нет соединения с VK API / сетевая ошибка"
	case vkAuthRateLimit:
		return "VK ограничил частоту запросов. Повторите подключение позже"
	case vkAuthCaptcha:
		return "VK требует прохождения капчи"
	case vkAuthInvalidClient:
		return "VK отклонил данные клиентского приложения"
	case vkAuthCallUnavailable:
		return "VK-звонок недоступен или ссылка недействительна"
	case vkAuthCanceled:
		return "Подключение отменено"
	default:
		return "Не удалось получить VK-креды"
	}
}

func traceVKAuthAttempt(streamID int, stage, endpoint string, attempt int, started time.Time, err error) {
	kind := classifyVKAuthError(err)
	duration := time.Since(started)
	// endpoint must be an explicitly selected hostname, never a URL.
	if strings.ContainsAny(endpoint, "/?@") {
		endpoint = "unknown"
	}
	log.Printf("[STREAM %d] [VK Auth] stage=%s error_kind=%s duration_ms=%d attempt=%d endpoint=%s",
		streamID, stage, kind, duration.Milliseconds(), attempt, endpoint)
	if c := getActiveCore(); c != nil {
		c.emitDiagnostic("DEBUG", "VK_AUTH", stage, string(kind),
			fmt.Sprintf("attempt=%d endpoint=%s", attempt, endpoint), streamID, duration)
	}
}

// Only a credential-specific VK rejection justifies trying the next client_id.
// Transport errors, API rate limits and captchas are never reasons to rotate.
func tryVKCredentialPairs(
	ctx context.Context,
	credentials []VKCredentials,
	run func(context.Context, VKCredentials, int) (string, string, []string, error),
) (string, string, []string, error) {
	if len(credentials) == 0 {
		return "", "", nil, errors.New("VK credentials not configured")
	}
	limit := len(credentials)
	if limit > vkCredentialAttemptLimit {
		limit = vkCredentialAttemptLimit
	}
	var lastErr error
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return "", "", nil, err
		}
		user, pass, addrs, err := run(ctx, credentials[i], i+1)
		if err == nil {
			return user, pass, addrs, nil
		}
		lastErr = err
		if classifyVKAuthError(err) != vkAuthInvalidClient {
			return "", "", nil, err
		}
	}
	return "", "", nil, fmt.Errorf("all VK credentials rejected: %w", lastErr)
}
