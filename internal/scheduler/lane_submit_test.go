package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// An expired ctx must not lose a free slot: select used to pick randomly between
// ctx.Done and the free token, so this failed about half of the iterations.
func TestLane_SubmitExpiredCtxStillTakesFreeSlot(t *testing.T) {
	lane := NewLane("test", 1)
	defer lane.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for i := 0; i < 50; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		if err := lane.Submit(ctx, wg.Done); err != nil {
			t.Fatalf("iteration %d: Submit with a free slot returned %v", i, err)
		}
		wg.Wait()
	}
}

// When the lane is full, the ctx still bounds the wait.
func TestLane_SubmitFullLaneHonoursCtx(t *testing.T) {
	lane := NewLane("test", 1)
	defer lane.Stop()

	release := make(chan struct{})
	if err := lane.Submit(context.Background(), func() { <-release }); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := lane.Submit(ctx, func() {}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Submit on a full lane = %v, want context.DeadlineExceeded", err)
	}
}
