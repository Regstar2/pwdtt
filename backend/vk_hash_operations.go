package backend

import (
	"context"
	"errors"
	"sync"
)

const vkHashBulkConcurrency = 2

type vkHashProbeCall struct {
	done   chan struct{}
	result VKHashCheckResult
	err    error
}

type vkHashOperationState struct {
	mu            sync.Mutex
	inFlight      map[string]*vkHashProbeCall
	probeSem      chan struct{}
	bulkCancel    context.CancelFunc
	bulkOperation string
	manualCancels map[string]context.CancelFunc
}

func newVKHashOperationState() *vkHashOperationState {
	return &vkHashOperationState{
		inFlight:      make(map[string]*vkHashProbeCall),
		probeSem:      make(chan struct{}, vkHashBulkConcurrency),
		manualCancels: make(map[string]context.CancelFunc),
	}
}

func vkHashProbeKey(id, profileName string) string {
	return profileName + "\x00" + id
}

func (s *vkHashOperationState) run(
	ctx context.Context,
	key string,
	probe func(context.Context) (VKHashCheckResult, error),
) (VKHashCheckResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if existing := s.inFlight[key]; existing != nil {
		s.mu.Unlock()
		select {
		case <-existing.done:
			return existing.result, existing.err
		case <-ctx.Done():
			return VKHashCheckResult{}, ctx.Err()
		}
	}
	call := &vkHashProbeCall{done: make(chan struct{})}
	s.inFlight[key] = call
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inFlight, key)
		close(call.done)
		s.mu.Unlock()
	}()

	select {
	case s.probeSem <- struct{}{}:
		defer func() { <-s.probeSem }()
	case <-ctx.Done():
		call.err = ctx.Err()
		return VKHashCheckResult{}, call.err
	}

	call.result, call.err = probe(ctx)
	return call.result, call.err
}

func (s *vkHashOperationState) beginBulk(parent context.Context) (context.Context, string, func(), error) {
	if parent == nil {
		parent = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bulkCancel != nil {
		return nil, "", nil, errors.New("проверка всех VK-хешей уже выполняется")
	}
	ctx, cancel := context.WithCancel(parent)
	operationID := newOperationID("hash-bulk")
	s.bulkCancel = cancel
	s.bulkOperation = operationID
	return ctx, operationID, func() {
		cancel()
		s.mu.Lock()
		if s.bulkOperation == operationID {
			s.bulkCancel = nil
			s.bulkOperation = ""
		}
		s.mu.Unlock()
	}, nil
}

func (s *vkHashOperationState) cancelBulk() {
	s.mu.Lock()
	cancel := s.bulkCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *vkHashOperationState) beginManual(parent context.Context) (context.Context, string, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	operationID := newOperationID("hash")
	s.mu.Lock()
	s.manualCancels[operationID] = cancel
	s.mu.Unlock()
	return ctx, operationID, func() {
		cancel()
		s.mu.Lock()
		delete(s.manualCancels, operationID)
		s.mu.Unlock()
	}
}

func (s *vkHashOperationState) cancelInteractive() {
	s.mu.Lock()
	bulkCancel := s.bulkCancel
	cancels := make([]context.CancelFunc, 0, len(s.manualCancels))
	for _, cancel := range s.manualCancels {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()

	if bulkCancel != nil {
		bulkCancel()
	}
	for _, cancel := range cancels {
		cancel()
	}
}
