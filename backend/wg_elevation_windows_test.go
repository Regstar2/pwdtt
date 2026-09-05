//go:build windows

package backend

import (
	"encoding/hex"
	"strings"
	"syscall"
	"testing"
)

func TestNewWindowsWGHelperToken(t *testing.T) {
	token, err := newWindowsWGHelperToken()
	if err != nil {
		t.Fatalf("newWindowsWGHelperToken: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64", len(token))
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Fatalf("token is not hex: %v", err)
	}
}

func TestFriendlyWindowsElevationErrorCancellation(t *testing.T) {
	err := friendlyWindowsElevationError(syscall.Errno(1223))
	if err == nil || !strings.Contains(err.Error(), "UAC отменён") {
		t.Fatalf("friendlyWindowsElevationError = %v, want clear UAC cancellation message", err)
	}
}
