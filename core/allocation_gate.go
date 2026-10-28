package core

import (
	"context"
	"sync"
	"time"
)

// allocationGate is owned by one Core and throttles its TURN Allocate calls.
type allocationGate struct {
	mu sync.Mutex
	next time.Time
	interval time.Duration
}
func newAllocationGate(interval time.Duration) *allocationGate {
	return &allocationGate{interval: interval}
}
func (g *allocationGate) Wait(ctx context.Context) error {
	g.mu.Lock()
	due := time.Now()
	if g.next.After(due) { due = g.next }
	g.next = due.Add(g.interval)
	g.mu.Unlock()
	if wait := time.Until(due); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done(): return ctx.Err()
		}
	}
	return ctx.Err()
}
