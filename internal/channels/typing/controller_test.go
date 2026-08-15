package typing

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestTTLAutoStop(t *testing.T) {
	var startCount, stopCount atomic.Int32

	ctrl := New(Options{
		MaxDuration:       100 * time.Millisecond,
		KeepaliveInterval: 0,
		StartFn:           func() error { startCount.Add(1); return nil },
		StopFn:            func() error { stopCount.Add(1); return nil },
	})

	ctrl.Start()

	// Should have started once
	if startCount.Load() != 1 {
		t.Fatalf("expected 1 start call, got %d", startCount.Load())
	}

	// Wait for TTL to expire
	time.Sleep(200 * time.Millisecond)

	if stopCount.Load() != 1 {
		t.Fatalf("expected 1 stop call after TTL, got %d", stopCount.Load())
	}

	// Verify closed
	ctrl.mu.Lock()
	closed := ctrl.closed
	ctrl.mu.Unlock()
	if !closed {
		t.Fatal("expected controller to be closed after TTL")
	}
}

func TestPostCloseGuard(t *testing.T) {
	var startCount atomic.Int32

	ctrl := New(Options{
		MaxDuration: 10 * time.Second,
		StartFn:     func() error { startCount.Add(1); return nil },
	})

	ctrl.Start()
	if startCount.Load() != 1 {
		t.Fatalf("expected 1 start call, got %d", startCount.Load())
	}

	ctrl.Stop()

	// Start after stop should be no-op
	ctrl.Start()
	if startCount.Load() != 1 {
		t.Fatalf("expected no additional start calls after Stop, got %d", startCount.Load())
	}
}

func TestDualSignalsRequired(t *testing.T) {
	var stopCount atomic.Int32

	ctrl := New(Options{
		MaxDuration: 10 * time.Second,
		StartFn:     func() error { return nil },
		StopFn:      func() error { stopCount.Add(1); return nil },
	})

	ctrl.Start()

	// Only run complete — should NOT stop
	ctrl.MarkRunComplete()
	time.Sleep(20 * time.Millisecond)
	if stopCount.Load() != 0 {
		t.Fatal("expected no stop after only MarkRunComplete")
	}

	// Now dispatch idle — should trigger cleanup
	ctrl.MarkDispatchIdle()
	time.Sleep(20 * time.Millisecond)
	if stopCount.Load() != 1 {
		t.Fatalf("expected 1 stop after both signals, got %d", stopCount.Load())
	}
}

func TestDualSignalsReverseOrder(t *testing.T) {
	var stopCount atomic.Int32

	ctrl := New(Options{
		MaxDuration: 10 * time.Second,
		StartFn:     func() error { return nil },
		StopFn:      func() error { stopCount.Add(1); return nil },
	})

	ctrl.Start()

	// Dispatch idle first — should NOT stop
	ctrl.MarkDispatchIdle()
	time.Sleep(20 * time.Millisecond)
	if stopCount.Load() != 0 {
		t.Fatal("expected no stop after only MarkDispatchIdle")
	}

	// Now run complete — should trigger cleanup
	ctrl.MarkRunComplete()
	time.Sleep(20 * time.Millisecond)
	if stopCount.Load() != 1 {
		t.Fatalf("expected 1 stop after both signals, got %d", stopCount.Load())
	}
}

func TestKeepalive(t *testing.T) {
	var startCount atomic.Int32

	ctrl := New(Options{
		MaxDuration:       2 * time.Second,
		KeepaliveInterval: 30 * time.Millisecond,
		StartFn:           func() error { startCount.Add(1); return nil },
	})

	ctrl.Start()
	time.Sleep(120 * time.Millisecond)
	ctrl.Stop()

	// Initial start + at least 2 keepalive ticks
	count := startCount.Load()
	if count < 3 {
		t.Fatalf("expected at least 3 start calls (1 initial + keepalive ticks), got %d", count)
	}
}

func TestKeepaliveStopsAfterClose(t *testing.T) {
	var startCount atomic.Int32

	ctrl := New(Options{
		MaxDuration:       2 * time.Second,
		KeepaliveInterval: 20 * time.Millisecond,
		StartFn:           func() error { startCount.Add(1); return nil },
	})

	ctrl.Start()
	time.Sleep(60 * time.Millisecond)
	ctrl.Stop()
	countAtStop := startCount.Load()

	// Wait to verify no more keepalive ticks
	time.Sleep(80 * time.Millisecond)
	countAfter := startCount.Load()

	if countAfter != countAtStop {
		t.Fatalf("expected no start calls after Stop, got %d more", countAfter-countAtStop)
	}
}

func TestStopIdempotent(t *testing.T) {
	var stopCount atomic.Int32

	ctrl := New(Options{
		MaxDuration: 10 * time.Second,
		StartFn:     func() error { return nil },
		StopFn:      func() error { stopCount.Add(1); return nil },
	})

	ctrl.Start()
	ctrl.Stop()
	ctrl.Stop()
	ctrl.Stop()

	if stopCount.Load() != 1 {
		t.Fatalf("expected exactly 1 stop call, got %d", stopCount.Load())
	}
}

func TestNilFunctions(t *testing.T) {
	// Should not panic with nil start/stop functions
	ctrl := New(Options{
		MaxDuration: 50 * time.Millisecond,
	})
	ctrl.Start()
	time.Sleep(100 * time.Millisecond)
	ctrl.Stop()
}
