package core

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// An injected credentials provider allows this scenario to exercise the real
// WorkerGroup -> Core.Stop -> event path without contacting VK or using a VPN.
func TestVKCredentialFailureStopsAllGroupsAndReportsDNS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	core := New(Config{})
	core.cancel = cancel
	before := getActiveCore()
	setActiveCore(core)
	t.Cleanup(func() { setActiveCore(before) })

	var attempted atomic.Int32
	credentials := func(context.Context, string, int) (string, string, []string, error) {
		attempted.Add(1)
		dns := &net.DNSError{Err: "i/o timeout", Name: "api.vk.me", IsTimeout: true}
		return "", "", nil, newVKAuthTransportError(ctx, "step1", "api.vk.me", dns)
	}
	params := &TurnParams{
		Hashes: []string{"fake-join-hash"},
		GetCredentials: credentials,
	}
	var pauseFlag int32
	pendingGroup := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		WorkerGroup(ctx, 2, 0, params, nil, nil, "", false, nil, nil, nil,
			&pauseFlag, "", "", nil, pendingGroup, nil, nil, nil)
	}()

	WorkerGroup(ctx, 1, 0, params, nil, nil, "", true, nil, nil, nil,
		&pauseFlag, "", "", nil, nil, nil, nil, nil)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("credential failure did not stop the core")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("another credential group remained blocked after failure")
	}
	if attempted.Load() != 1 {
		t.Fatalf("credential provider called %d times; expected once", attempted.Load())
	}

	const want = "Не удалось разрешить адрес VK API / проблема DNS"
	seenFatal := false
	seenProgress := false
	for {
		select {
		case ev := <-core.events:
			switch ev.Type {
			case EventError:
				if ev.Name == "VK_CREDENTIALS" && ev.Message == want {
					seenFatal = true
				}
			case EventEvent:
				if ev.Name != "connection_progress" {
					continue
				}
				var payload struct { Stage, State, Message string }
				if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Stage == "vk" && payload.State == "error" && payload.Message == want {
					seenProgress = true
				}
			}
		default:
			if !seenFatal || !seenProgress {
				t.Fatalf("expected terminal VK error and UI progress; fatal=%t progress=%t", seenFatal, seenProgress)
			}
			return
		}
	}
}

func TestVKCredentialGateCancelBeforeRequest(t *testing.T) {
	select {
	case vkRequestGate <- struct{}{}:
	default:
		t.Fatal("credential gate already locked")
	}
	defer func() { <-vkRequestGate }()

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, _, err := fetchVkCredsSerialized(ctx, "fake-hash", 123)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline; got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("global serialization gate ignored request context deadline")
	}
}

func TestVKDNSTimeoutDoesNotInvalidateHash(t *testing.T) {
	dns := &net.DNSError{Err: "request timeout", Name: "api.vk.me", IsTimeout: true}
	err := newVKAuthTransportError(context.Background(), "step1", "api.vk.me", dns)
	outcome := classifyHashProbeCredentialError(err, time.Now())
	if outcome.Status != HashProbeError || outcome.ErrorType != HashProbeErrorNetwork {
		t.Fatalf("network timeout must not mark a VK hash invalid, result=%+v", outcome)
	}
	if strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("transport error leaked sensitive URL: %v", err)
	}
}
