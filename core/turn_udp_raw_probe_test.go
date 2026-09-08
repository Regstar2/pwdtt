package core

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestProbeTurnUDPRawRejectsEmptyHash(t *testing.T) {
	_, err := ProbeTurnUDPRaw(context.Background(), TurnUDPRawProbeConfig{
		Target:  "127.0.0.1:9",
		Payload: []byte{1},
	})
	if err == nil || !strings.Contains(err.Error(), "VK hash is required") {
		t.Fatalf("expected VK hash validation error, got %v", err)
	}
}

func TestProbeTurnUDPRawRejectsEmptyTarget(t *testing.T) {
	_, err := ProbeTurnUDPRaw(context.Background(), TurnUDPRawProbeConfig{
		Hash:    "dummy",
		Payload: []byte{1},
	})
	if err == nil || !strings.Contains(err.Error(), "UDP target is required") {
		t.Fatalf("expected target validation error, got %v", err)
	}
}

func TestProbeTurnUDPRawRejectsEmptyPayload(t *testing.T) {
	_, err := ProbeTurnUDPRaw(context.Background(), TurnUDPRawProbeConfig{
		Hash:   "dummy",
		Target: "127.0.0.1:9",
	})
	if err == nil || !strings.Contains(err.Error(), "UDP payload is required") {
		t.Fatalf("expected payload validation error, got %v", err)
	}
}

func TestProbeTurnUDPRawRejectsOversizedPayload(t *testing.T) {
	_, err := ProbeTurnUDPRaw(context.Background(), TurnUDPRawProbeConfig{
		Hash:    "dummy",
		Target:  "127.0.0.1:9",
		Payload: bytes.Repeat([]byte{1}, maxTurnUDPRawProbePayload+1),
	})
	if err == nil || !strings.Contains(err.Error(), "UDP payload is too large") {
		t.Fatalf("expected payload size validation error, got %v", err)
	}
}

func TestTurnUDPRawHexPreview(t *testing.T) {
	data := bytes.Repeat([]byte{0xab}, turnUDPRawPreviewBytes+10)
	got := turnUDPRawHexPreview(data)
	if len(got) != turnUDPRawPreviewBytes*2 {
		t.Fatalf("preview hex length=%d, want %d", len(got), turnUDPRawPreviewBytes*2)
	}
	if strings.Contains(got, " ") {
		t.Fatalf("preview must be compact hex, got %q", got)
	}
}
