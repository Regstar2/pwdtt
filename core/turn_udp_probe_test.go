package core

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func TestProbeTurnUDPRejectsEmptyHash(t *testing.T) {
	_, err := ProbeTurnUDP(context.Background(), TurnUDPProbeConfig{})
	if err == nil || !strings.Contains(err.Error(), "VK hash is required") {
		t.Fatalf("expected VK hash validation error, got %v", err)
	}
}

func TestBuildDNSAQuery(t *testing.T) {
	query, transactionID, err := buildDNSAQuery("example.com")
	if err != nil {
		t.Fatalf("buildDNSAQuery failed: %v", err)
	}
	if len(query) < 12 {
		t.Fatalf("query too short: %d", len(query))
	}
	if got := binary.BigEndian.Uint16(query[0:2]); got != transactionID {
		t.Fatalf("transaction ID mismatch: header=%d returned=%d", got, transactionID)
	}
	if got := binary.BigEndian.Uint16(query[4:6]); got != 1 {
		t.Fatalf("QDCOUNT=%d, want 1", got)
	}
}

func TestValidateDNSResponse(t *testing.T) {
	const transactionID = 0x1234
	response := make([]byte, 12)
	binary.BigEndian.PutUint16(response[0:2], transactionID)
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[6:8], 1)

	answers, rcode, err := validateDNSResponse(response, transactionID)
	if err != nil {
		t.Fatalf("validateDNSResponse failed: %v", err)
	}
	if answers != 1 {
		t.Fatalf("answers=%d, want 1", answers)
	}
	if rcode != 0 {
		t.Fatalf("rcode=%d, want 0", rcode)
	}
}

func TestValidateDNSResponseRejectsWrongTransactionID(t *testing.T) {
	response := make([]byte, 12)
	binary.BigEndian.PutUint16(response[0:2], 0x1111)
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[6:8], 1)

	_, _, err := validateDNSResponse(response, 0x2222)
	if err == nil || !strings.Contains(err.Error(), "transaction ID mismatch") {
		t.Fatalf("expected transaction ID error, got %v", err)
	}
}
