package skills

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPackageLock_AcquireRelease(t *testing.T) {
	l := NewPackageLocker()
	ctx := context.Background()
	r, err := l.Acquire(ctx, "github", "lazygit")
	if err != nil {
		t.Fatal(err)
	}
	r()
	// Re-acquiring after release should succeed quickly.
	r2, err := l.Acquire(ctx, "github", "lazygit")
	if err != nil {
		t.Fatal(err)
	}
	r2()
}

func TestPackageLock_ReleaseIdempotent(t *testing.T) {
	l := NewPackageLocker()
	r, _ := l.Acquire(context.Background(), "github", "gh")
	r()
	r() // second call must not panic
}

func TestPackageLock_SameKey_Serializes(t *testing.T) {
	l := NewPackageLocker()
	var inFlight int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := l.Acquire(context.Background(), "github", "same")
			if err != nil {
				t.Error(err)
				return
			}
			cur := atomic.AddInt32(&inFlight, 1)
			// Track peak concurrency — MUST stay at 1.
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if cur <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			r()
		}()
	}
	wg.Wait()
	if maxConcurrent != 1 {
		t.Fatalf("expected max concurrency 1, got %d", maxConcurrent)
	}
}

func TestPackageLock_DifferentKeys_Parallel(t *testing.T) {
	l := NewPackageLocker()
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	for _, name := range []string{"a", "b"} {
		n := name
		go func() {
			r, err := l.Acquire(context.Background(), "github", n)
			if err != nil {
				t.Error(err)
				return
			}
			started <- struct{}{}
			<-release
			r()
		}()
	}
	// Both goroutines should acquire without blocking.
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-timer.C:
			t.Fatal("expected both keys to acquire independently")
		}
	}
	close(release)
}

func TestPackageLock_Acquire_CtxCancel(t *testing.T) {
	l := NewPackageLocker()
	// Hold the lock.
	held, _ := l.Acquire(context.Background(), "github", "held")
	defer held()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r, err := l.Acquire(ctx, "github", "held")
	if err == nil {
		r()
		t.Fatal("expected ctx-deadline error")
	}
	if !isCancelErr(err) {
		t.Fatalf("expected ctx error, got %v", err)
	}
}

func TestPackageLock_TryAcquire(t *testing.T) {
	l := NewPackageLocker()
	r1, ok := l.TryAcquire("github", "x")
	if !ok {
		t.Fatal("first TryAcquire should succeed")
	}
	// Second try while held should fail immediately.
	if _, ok := l.TryAcquire("github", "x"); ok {
		t.Fatal("second TryAcquire on held key should fail")
	}
	r1()
	// After release, try should succeed again.
	r2, ok := l.TryAcquire("github", "x")
	if !ok {
		t.Fatal("TryAcquire after release should succeed")
	}
	r2()
}

func isCancelErr(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
