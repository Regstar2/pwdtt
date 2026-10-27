package core

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestClassifyTurnAllocateError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want SessionErrorType
	}{
		{name: "all retransmissions failed", err: errors.New("all retransmissions failed for 5GuKYeHgZVBYckr5"), want: SessionErrorAddressDead},
		{name: "quota", err: errors.New("allocation quota reached"), want: SessionErrorAddressDead},
		{name: "timeout", err: errors.New("transaction timeout"), want: SessionErrorAddressDead},
		{name: "fatal auth", err: errors.New("401 unauthorized"), want: SessionErrorFatal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTurnAllocateError(tc.err); got != tc.want {
				t.Fatalf("classifyTurnAllocateError() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSelectTurnAddressRoundRobin(t *testing.T) {
	available := []string{"turn-a", "turn-b", "turn-c"}
	want := []string{"turn-a", "turn-b", "turn-c", "turn-a", "turn-b", "turn-c"}

	for workerID, expected := range want {
		if got := selectTurnAddress(available, workerID+1); got != expected {
			t.Fatalf("worker %d selected %q, want %q", workerID+1, got, expected)
		}
	}
}

func TestTurnParamsForWorkerDoesNotMutateSharedObfsMode(t *testing.T) {
	base := &TurnParams{
		Host:     "127.0.0.1",
		Port:     "19302",
		Hashes:   []string{"hash"},
		WrapKey:  make([]byte, wrapKeyLen),
		ObfsMode: "audio",
	}

	const workers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			mode := "audio"
			if i%2 == 1 {
				mode = "video"
			}
			local := turnParamsForWorker(base, mode)
			if local.ObfsMode != mode {
				t.Errorf("worker-local ObfsMode = %q, want %q", local.ObfsMode, mode)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if base.ObfsMode != "audio" {
		t.Fatalf("shared ObfsMode mutated to %q", base.ObfsMode)
	}
}

func TestDTLSTimeoutClassification(t *testing.T) {
	if !isDTLSTimeout(context.DeadlineExceeded) || !isDTLSTimeout(os.ErrDeadlineExceeded) || !isDTLSTimeout(errors.New("i/o timeout")) {
		t.Fatal("timeout must be classified as an unavailable handshake, not TURN failure")
	}
	if isDTLSTimeout(errors.New("bad certificate")) || isDTLSTimeout(nil) {
		t.Fatal("non-timeout must not be classified as WRAP timeout")
	}
}

func TestDTLSRelayTrafficObservations(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 3344}
	var traffic dtlsRelayTraffic
	if got := traffic.hint(); got != "no_outbound_relay_packets" {
		t.Fatalf("unexpected initial hint: %s", got)
	}
	traffic.sent.Add(1)
	if got := traffic.hint(); got != "no_inbound_relay_packets" {
		t.Fatalf("no inbound relay hint: %s", got)
	}
	traffic.received.Add(1)
	if got := traffic.hint(); got != "no_inbound_from_expected_peer" {
		t.Fatalf("wrong peer hint: %s", got)
	}
	traffic.fromPeer.Add(1)
	traffic.rejected.Add(1)
	if got := traffic.hint(); got != "inbound_wrap_not_verified" {
		t.Fatalf("WRAP mismatch hint: %s", got)
	}
	traffic.accepted.Add(1)
	if got := traffic.hint(); got != "inbound_forwarded_to_dtls" {
		t.Fatalf("forwarded hint: %s", got)
	}
	summary := traffic.summary(peer, "audio")
	if !strings.Contains(summary, "peer_family=ipv4 peer_port=3344") || !strings.Contains(summary, "accepted=1 rejected=1") {
		t.Fatalf("missing safe counters: %s", summary)
	}
	if strings.Contains(summary, peer.IP.String()) {
		t.Fatalf("diagnostic leaked peer IP: %s", summary)
	}
}

func TestAllDTLSAttemptsExhausted(t *testing.T) {
	urls := []string{"relay1", "relay2"}
	cases := []struct {
		counts map[string]int
		want bool
	}{
		{counts: map[string]int{}, want: false},
		{counts: map[string]int{"relay1": 2}, want: false},
		{counts: map[string]int{"relay1": 2, "relay2": 1}, want: false},
		{counts: map[string]int{"relay1": 2, "relay2": 2}, want: true},
	}
	for _, c := range cases {
		if got := allDTLSAttemptsExhausted(urls, c.counts); got != c.want {
			t.Fatalf("counts=%v exhausted=%v want=%v", c.counts, got, c.want)
		}
	}
	if allDTLSAttemptsExhausted(nil, map[string]int{}) {
		t.Fatal("empty TURN list must not be mistaken for DTLS exhaustion")
	}
}
