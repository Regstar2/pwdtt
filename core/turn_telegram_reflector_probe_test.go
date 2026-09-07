package core

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func TestProbeTelegramReflectorRejectsEmptyHash(t *testing.T) {
	_, err := ProbeTelegramReflector(context.Background(), TelegramReflectorProbeConfig{})
	if err == nil || !strings.Contains(err.Error(), "VK hash is required") {
		t.Fatalf("expected VK hash validation error, got %v", err)
	}
}

func TestBuildTelegramReflectorHello(t *testing.T) {
	const peerTag = "00112233445566778899aabbccddeeff"

	packet, prefix, err := buildTelegramReflectorHello(peerTag)
	if err != nil {
		t.Fatalf("buildTelegramReflectorHello failed: %v", err)
	}
	if len(packet) != 40 {
		t.Fatalf("packet length=%d, want 40", len(packet))
	}
	if got := len(prefix); got != 12 {
		t.Fatalf("prefix length=%d, want 12", got)
	}
	if packet[28] != 0xfe {
		t.Fatalf("special marker=%#x, want 0xfe", packet[28])
	}
	if got := binary.BigEndian.Uint64(packet[32:40]); got != 123 {
		t.Fatalf("ping value=%d, want 123", got)
	}
}

func TestBuildTelegramReflectorHelloRejectsInvalidPeerTag(t *testing.T) {
	_, _, err := buildTelegramReflectorHello("0011")
	if err == nil || !strings.Contains(err.Error(), "exactly 16 bytes") {
		t.Fatalf("expected peer_tag length error, got %v", err)
	}
}
