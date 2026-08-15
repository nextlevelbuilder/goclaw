package agent

import (
	"context"
	"testing"
)

func TestParallelToolCollection_ContextCancel(t *testing.T) {
	// Simulate: 3 tool calls, but ctx cancelled before all arrive
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan indexedResult, 3)

	// Send only 1 result, then cancel
	resultCh <- indexedResult{idx: 0}
	cancel()

	collected := make([]indexedResult, 0, 3)
	var err error

collectLoop:
	for range 3 {
		select {
		case r, ok := <-resultCh:
			if !ok {
				break collectLoop
			}
			collected = append(collected, r)
		case <-ctx.Done():
			err = ctx.Err()
			break collectLoop
		}
	}

	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
	// We should have collected 1 result before cancellation (or 0 — timing dependent)
	if len(collected) > 1 {
		t.Errorf("expected at most 1 collected, got %d", len(collected))
	}
}

func TestParallelToolCollection_AllComplete(t *testing.T) {
	resultCh := make(chan indexedResult, 3)
	resultCh <- indexedResult{idx: 0}
	resultCh <- indexedResult{idx: 1}
	resultCh <- indexedResult{idx: 2}

	ctx := context.Background()
	collected := make([]indexedResult, 0, 3)

collectLoop:
	for range 3 {
		select {
		case r, ok := <-resultCh:
			if !ok {
				break collectLoop
			}
			collected = append(collected, r)
		case <-ctx.Done():
			t.Fatal("context should not be cancelled")
		}
	}

	if len(collected) != 3 {
		t.Fatalf("expected 3, got %d", len(collected))
	}
}
