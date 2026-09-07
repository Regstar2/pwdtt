package core

import (
	"context"
	"strings"
	"testing"
)

func TestProbeTurnTCPRejectsEmptyHash(t *testing.T) {
	_, err := ProbeTurnTCP(context.Background(), TurnTCPProbeConfig{
		Target: "example.com:80",
	})
	if err == nil || !strings.Contains(err.Error(), "VK hash is required") {
		t.Fatalf("expected VK hash validation error, got %v", err)
	}
}

func TestProbeTurnTCPRejectsInvalidTarget(t *testing.T) {
	_, err := ProbeTurnTCP(context.Background(), TurnTCPProbeConfig{
		Hash:   "test-hash",
		Target: "missing-port",
	})
	if err == nil || !strings.Contains(err.Error(), "resolve target") {
		t.Fatalf("expected target validation error, got %v", err)
	}
}
