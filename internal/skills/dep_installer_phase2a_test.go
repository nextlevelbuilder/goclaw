package skills

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSharedLocker_InstallAndUpdateSerialize is a P2A-C2 regression guard.
//
// It simulates two concurrent paths that must serialize on the same pip package:
//   - Goroutine A: mimics InstallSingleDep acquiring the shared locker for pip "requests"
//   - Goroutine B: mimics PipUpdateExecutor.Update acquiring via UpdateRegistry.Apply
//     for the same source+pkg key
//
// Both paths call sharedPackageLocker().Acquire(ctx, "pip", "requests").
// Asserts: goroutine B blocks until A releases; peak concurrency = 1; no -race.
func TestSharedLocker_InstallAndUpdateSerialize(t *testing.T) {
	t.Cleanup(func() { sharedLocker.Store(nil) })

	l := NewPackageLocker()
	SetSharedPackageLocker(l)

	const source = "pip"
	const pkg = "requests"

	var inFlight int32
	var maxConcurrent int32
	var order []string
	var orderMu sync.Mutex

	recordIn := func(label string) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			m := atomic.LoadInt32(&maxConcurrent)
			if cur <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, cur) {
				break
			}
		}
		orderMu.Lock()
		order = append(order, label+":in")
		orderMu.Unlock()
	}
	recordOut := func(label string) {
		atomic.AddInt32(&inFlight, -1)
		orderMu.Lock()
		order = append(order, label+":out")
		orderMu.Unlock()
	}

	// A acquires first; B must wait until A is done.
	releaseCh := make(chan struct{})
	aHolding := make(chan struct{})

	var wg sync.WaitGroup

	// Goroutine A — simulates InstallSingleDep pip path.
	wg.Add(1)
	go func() {
		defer wg.Done()
		locker := sharedPackageLocker()
		if locker == nil {
			t.Errorf("A: sharedPackageLocker() is nil")
			return
		}
		release, err := locker.Acquire(context.Background(), source, pkg)
		if err != nil {
			t.Errorf("A: Acquire failed: %v", err)
			return
		}
		recordIn("A")
		close(aHolding) // signal B that A is now holding the lock
		<-releaseCh     // hold until test signals
		recordOut("A")
		release()
	}()

	// Wait until A is holding the lock before starting B.
	<-aHolding

	// Goroutine B — simulates UpdateRegistry.Apply → PipUpdateExecutor path.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Use a shared locker directly (as the registry would).
		locker := sharedPackageLocker()
		if locker == nil {
			t.Errorf("B: sharedPackageLocker() is nil")
			return
		}
		release, err := locker.Acquire(context.Background(), source, pkg)
		if err != nil {
			t.Errorf("B: Acquire failed: %v", err)
			return
		}
		recordIn("B")
		time.Sleep(2 * time.Millisecond) // simulate work
		recordOut("B")
		release()
	}()

	// Let A proceed after a brief delay to ensure B is queued.
	time.Sleep(20 * time.Millisecond)
	close(releaseCh)

	wg.Wait()

	if maxConcurrent != 1 {
		t.Fatalf("expected max in-flight = 1, got %d — pip install+update are NOT serialized", maxConcurrent)
	}

	// Verify that A completed before B started (order: A:in, A:out, B:in, B:out).
	orderMu.Lock()
	defer orderMu.Unlock()
	if len(order) != 4 {
		t.Fatalf("expected 4 order events, got %d: %v", len(order), order)
	}
	if order[0] != "A:in" || order[1] != "A:out" || order[2] != "B:in" || order[3] != "B:out" {
		t.Errorf("unexpected order: %v (want [A:in A:out B:in B:out])", order)
	}
}
