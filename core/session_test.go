package core

import (
	"errors"
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
