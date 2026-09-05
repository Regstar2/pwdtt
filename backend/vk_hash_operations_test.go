package backend

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestVKHashOperationStateDeduplicatesInFlight(t *testing.T) {
	state := newVKHashOperationState()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	probe := func(context.Context) (VKHashCheckResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return VKHashCheckResult{HashID: "hash-1", Status: vkHashStatusValid}, nil
	}

	first := make(chan VKHashCheckResult, 1)
	go func() {
		result, _ := state.run(context.Background(), vkHashProbeKey("hash-1", "server"), probe)
		first <- result
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first probe did not start")
	}

	second := make(chan VKHashCheckResult, 1)
	go func() {
		result, _ := state.run(context.Background(), vkHashProbeKey("hash-1", "server"), probe)
		second <- result
	}()

	time.Sleep(20 * time.Millisecond)
	close(release)

	for i, ch := range []chan VKHashCheckResult{first, second} {
		select {
		case result := <-ch:
			if result.Status != vkHashStatusValid {
				t.Fatalf("result %d status=%q", i, result.Status)
			}
		case <-time.After(time.Second):
			t.Fatalf("result %d timed out", i)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("probe calls=%d, want 1", got)
	}
}

func TestVKHashOperationStateCancelBulk(t *testing.T) {
	state := newVKHashOperationState()
	ctx, _, done, err := state.beginBulk(context.Background())
	if err != nil {
		t.Fatalf("beginBulk() error = %v", err)
	}

	state.cancelBulk()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("bulk context was not canceled")
	}
	done()

	_, _, doneAgain, err := state.beginBulk(context.Background())
	if err != nil {
		t.Fatalf("second beginBulk() error = %v", err)
	}
	doneAgain()
}

func TestVKHashOperationStateRetriesCanceledSharedProbe(t *testing.T) {
	state := newVKHashOperationState()
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	var calls atomic.Int32

	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = state.run(firstCtx, vkHashProbeKey("hash-1", "server"), func(ctx context.Context) (VKHashCheckResult, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
			}
			<-firstRelease
			return VKHashCheckResult{}, ctx.Err()
		})
	}()

	<-firstStarted
	firstCancel()

	secondDone := make(chan VKHashCheckResult, 1)
	go func() {
		result, _ := state.run(context.Background(), vkHashProbeKey("hash-1", "server"), func(context.Context) (VKHashCheckResult, error) {
			calls.Add(1)
			return VKHashCheckResult{Status: vkHashStatusValid}, nil
		})
		secondDone <- result
	}()

	close(firstRelease)
	<-firstDone
	select {
	case result := <-secondDone:
		if result.Status != vkHashStatusValid {
			t.Fatalf("second status=%q", result.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("second probe did not retry")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("probe calls=%d, want 2", got)
	}
}
