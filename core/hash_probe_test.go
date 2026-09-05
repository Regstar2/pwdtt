package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHashProbeCallUnavailableIsInvalid(t *testing.T) {
	result := classifyHashProbeCredentialError(&CallUnavailableError{Code: 951, Message: "gone"}, time.Now())
	if result.Status != HashProbeInvalid || result.ErrorType != HashProbeErrorInvalidHash {
		t.Fatalf("status=%q errorType=%q", result.Status, result.ErrorType)
	}
}

func TestHashProbeNetworkFailureIsNotInvalid(t *testing.T) {
	result := classifyHashProbeCredentialError(errors.New("connection refused"), time.Now())
	if result.Status != HashProbeError {
		t.Fatalf("status=%q, want error", result.Status)
	}
}

func TestHashProbeWrapTimeoutIsNotInvalid(t *testing.T) {
	result := classifyHashProbeSessionError(&SessionError{
		Type: SessionErrorWrapTimeout,
		Err:  errors.New("timeout"),
	}, time.Now())
	if result.Status != HashProbeError || result.ErrorType != HashProbeErrorWRAP {
		t.Fatalf("status=%q errorType=%q", result.Status, result.ErrorType)
	}
}

func TestHashProbeCancellationIsError(t *testing.T) {
	result := classifyHashProbeCredentialError(context.Canceled, time.Now())
	if result.Status != HashProbeError || result.ErrorType != HashProbeErrorCanceled {
		t.Fatalf("status=%q errorType=%q", result.Status, result.ErrorType)
	}
}

func TestHashProbeDeadlineIsTimeout(t *testing.T) {
	result := classifyHashProbeContextError(context.DeadlineExceeded, time.Now())
	if result.Status != HashProbeError || result.ErrorType != HashProbeErrorTimeout {
		t.Fatalf("status=%q errorType=%q", result.Status, result.ErrorType)
	}
}
