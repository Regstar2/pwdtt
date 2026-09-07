package core

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestBuildTelegramReqPQMulti(t *testing.T) {
	payload, nonce, err := buildTelegramReqPQMulti(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("buildTelegramReqPQMulti: %v", err)
	}
	if len(payload) != 40 {
		t.Fatalf("payload length=%d, want 40", len(payload))
	}
	if got := binary.LittleEndian.Uint64(payload[0:8]); got != 0 {
		t.Fatalf("auth_key_id=%d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(payload[16:20]); got != 20 {
		t.Fatalf("body length=%d, want 20", got)
	}
	if got := binary.LittleEndian.Uint32(payload[20:24]); got != 0xbe7e8ef1 {
		t.Fatalf("constructor=%#x, want req_pq_multi", got)
	}
	if !equalBytes(payload[24:40], nonce[:]) {
		t.Fatal("nonce not serialized into req_pq_multi")
	}
}

func TestWrapTelegramMTProtoUDP(t *testing.T) {
	payload := make([]byte, 40)

	cases := map[string]int{
		"raw":                      40,
		"abridged":                 41,
		"abridged-init":            42,
		"intermediate":             44,
		"intermediate-init":        48,
		"padded-intermediate-init": 48,
		"full":                     52,
	}

	for mode, wantLen := range cases {
		packet, err := wrapTelegramMTProtoUDP(mode, payload)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if len(packet) != wantLen {
			t.Fatalf("%s length=%d, want %d", mode, len(packet), wantLen)
		}
	}

	full, err := wrapTelegramMTProtoUDP("full", payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(full[0:4]); got != 52 {
		t.Fatalf("full length field=%d, want 52", got)
	}
}

func TestValidateTelegramResPQ(t *testing.T) {
	var nonce [16]byte
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}

	response := make([]byte, 20+4+16+8)
	binary.LittleEndian.PutUint64(response[0:8], 0)
	binary.LittleEndian.PutUint64(response[8:16], 123)
	binary.LittleEndian.PutUint32(response[16:20], 28)
	binary.LittleEndian.PutUint32(response[20:24], 0x05162463)
	copy(response[24:40], nonce[:])

	wrapped := append([]byte{0xee, 0xee, 0xee, 0xee, byte(len(response)), 0, 0, 0}, response...)
	if !validateTelegramResPQ(wrapped, nonce) {
		t.Fatal("expected valid resPQ")
	}

	nonce[0] ^= 0xff
	if validateTelegramResPQ(wrapped, nonce) {
		t.Fatal("accepted resPQ with wrong nonce")
	}
}
