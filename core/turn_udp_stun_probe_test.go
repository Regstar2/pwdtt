package core

import (
	"context"
	"strings"
	"testing"

	"github.com/pion/stun/v3"
)

func TestProbeTurnUDPSTUNRejectsEmptyHash(t *testing.T) {
	_, err := ProbeTurnUDPSTUN(context.Background(), TurnUDPSTUNProbeConfig{})
	if err == nil || !strings.Contains(err.Error(), "VK hash is required") {
		t.Fatalf("expected VK hash validation error, got %v", err)
	}
}

func TestSTUNBindingRequestHasTransactionID(t *testing.T) {
	request, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		t.Fatalf("build STUN request: %v", err)
	}
	if request.Type != stun.BindingRequest {
		t.Fatalf("request type=%s, want %s", request.Type, stun.BindingRequest)
	}
	if len(request.Raw) < 20 {
		t.Fatalf("request too short: %d", len(request.Raw))
	}
}
