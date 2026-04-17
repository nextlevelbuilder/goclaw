package skills

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSharedPackageLocker_NilPath verifies that when no shared locker is
// installed, sharedPackageLocker() returns nil (backward-compatible path).
func TestSharedPackageLocker_NilPath(t *testing.T) {
	// Clear any previously injected locker from other tests.
	sharedLocker.Store(nil)

	if got := sharedPackageLocker(); got != nil {
		t.Errorf("sharedPackageLocker() = %v, want nil when not set", got)
	}
}

// TestSetSharedPackageLocker_InjectsAndReturns verifies that
// SetSharedPackageLocker stores the locker and sharedPackageLocker retrieves it.
func TestSetSharedPackageLocker_InjectsAndReturns(t *testing.T) {
	t.Cleanup(func() { sharedLocker.Store(nil) }) // restore after test

	l := NewPackageLocker()
	SetSharedPackageLocker(l)

	got := sharedPackageLocker()
	if got == nil {
		t.Fatal("sharedPackageLocker() returned nil after SetSharedPackageLocker")
	}
	if got != l {
		t.Error("sharedPackageLocker() returned a different locker than injected")
	}
}

// TestSharedPackageLocker_Serializes verifies that when a shared locker is
// installed, concurrent calls for the same source+pkg key are serialized
// (at most one acquires at a time).
func TestSharedPackageLocker_Serializes(t *testing.T) {
	t.Cleanup(func() { sharedLocker.Store(nil) })

	l := NewPackageLocker()
	SetSharedPackageLocker(l)

	const goroutines = 8
	var inFlight int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := l.Acquire(context.Background(), "pip", "foo")
			if err != nil {
				t.Errorf("Acquire failed: %v", err)
				return
			}
			cur := atomic.AddInt32(&inFlight, 1)
			// Update peak concurrency.
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if cur <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			release()
		}()
	}
	wg.Wait()

	if maxConcurrent != 1 {
		t.Fatalf("expected max concurrency 1, got %d — locker is not serializing", maxConcurrent)
	}
}

// TestSharedPackageLocker_DifferentSources verifies that pip and npm keys are
// independent (different sources can hold locks concurrently).
func TestSharedPackageLocker_DifferentSources(t *testing.T) {
	t.Cleanup(func() { sharedLocker.Store(nil) })

	l := NewPackageLocker()
	SetSharedPackageLocker(l)

	started := make(chan struct{}, 2)
	done := make(chan struct{})

	go func() {
		release, err := l.Acquire(context.Background(), "pip", "requests")
		if err != nil {
			t.Errorf("pip Acquire: %v", err)
			return
		}
		started <- struct{}{}
		<-done
		release()
	}()

	go func() {
		release, err := l.Acquire(context.Background(), "npm", "requests")
		if err != nil {
			t.Errorf("npm Acquire: %v", err)
			return
		}
		started <- struct{}{}
		<-done
		release()
	}()

	// Both goroutines (different source keys) should acquire without blocking.
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-timer.C:
			t.Fatal("pip and npm locks should be independent — timed out waiting")
		}
	}
	close(done)
}
