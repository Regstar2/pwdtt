package core

import (
    "context"
    "errors"
    "testing"
    "time"
)

func TestAllocationGateSpacesAttempts(t *testing.T) {
    gate := newAllocationGate(30 * time.Millisecond)
    ctx := context.Background()
    start := time.Now()
    if err := gate.Wait(ctx); err != nil { t.Fatal(err) }
    if err := gate.Wait(ctx); err != nil { t.Fatal(err) }
    if elapsed := time.Since(start); elapsed < 25 * time.Millisecond {
        t.Fatalf("two allocations were not paced: %v", elapsed)
    }
}

func TestAllocationGateCancellationAndIsolation(t *testing.T) {
    gate := newAllocationGate(time.Second)
    if err := gate.Wait(context.Background()); err != nil { t.Fatal(err) }
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    start := time.Now()
    if err := gate.Wait(ctx); !errors.Is(err, context.Canceled) {
        t.Fatalf("cancel error = %v", err)
    }
    if time.Since(start) > 100 * time.Millisecond { t.Fatal("cancel stalled") }
    // Another Core's gate must not inherit the first gate's wait.
    if err := newAllocationGate(time.Second).Wait(context.Background()); err != nil { t.Fatal(err) }
}
