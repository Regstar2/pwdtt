package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParsePayloadHex(t *testing.T) {
	got, err := parsePayload("00ff10", "")
	if err != nil {
		t.Fatalf("parsePayload failed: %v", err)
	}
	if !bytes.Equal(got, []byte{0x00, 0xff, 0x10}) {
		t.Fatalf("payload=%x", got)
	}
}

func TestParsePayloadText(t *testing.T) {
	got, err := parsePayload("", "ping")
	if err != nil {
		t.Fatalf("parsePayload failed: %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("payload=%q", got)
	}
}

func TestParsePayloadRejectsBoth(t *testing.T) {
	_, err := parsePayload("00", "x")
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("expected exclusivity error, got %v", err)
	}
}

func TestNormalizeVKHashFullLink(t *testing.T) {
	got := normalizeVKHash("https://vk.com/call/join/abc123?foo=bar")
	if got != "abc123" {
		t.Fatalf("hash=%q, want abc123", got)
	}
}
