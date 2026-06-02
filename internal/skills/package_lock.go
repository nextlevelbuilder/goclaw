package skills

import (
	"context"
	"sync"
)

// PackageLocker serializes install/update/uninstall against the same package
// without blocking unrelated packages. Keys are free-form strings; callers
// SHOULD use "{source}:{name}" (e.g. "github:lazygit").
//
// Design (red-team H1):
//   - `map[string]*entry` guarded by an outer mutex for lookup/insert.
//   - Each entry is a channel-based mutex (buffered chan struct{} of size 1)
//     so Acquire can respect ctx cancellation / Done.
//   - Release is idempotent within a single Acquire; releasing a second time
//     is a no-op (never panics).
//
// Map entries are NOT garbage-collected. For a long-lived gateway with high
// install churn, memory growth is bounded by the number of distinct package
// names ever installed (typically < 1000). Not a Phase 1 concern — reassess
// at > 10k churn.
type PackageLocker struct {
	mu    sync.Mutex
	locks map[string]*packageLockEntry
}

type packageLockEntry struct {
	ch chan struct{}
}

// NewPackageLocker constructs a locker with an empty map.
func NewPackageLocker() *PackageLocker {
	return &PackageLocker{locks: make(map[string]*packageLockEntry)}
}

// lockKey derives the map key. Empty source/name accepted but discouraged.
func lockKey(source, name string) string {
	return source + ":" + name
}

// Acquire blocks until the lock for (source, name) is granted or ctx is done.
//
// On success returns a release func that MUST be called exactly once (call
// additional times are safe — they no-op). Callers SHOULD `defer release()`
// immediately after checking the error.
//
// On ctx cancellation returns (nil, ctx.Err()). The lock is NOT held.
func (l *PackageLocker) Acquire(ctx context.Context, source, name string) (func(), error) {
	l.mu.Lock()
	key := lockKey(source, name)
	e, ok := l.locks[key]
	if !ok {
		e = &packageLockEntry{ch: make(chan struct{}, 1)}
		l.locks[key] = e
	}
	l.mu.Unlock()

	// Try fast path first (uncontended case).
	select {
	case e.ch <- struct{}{}:
		return l.makeRelease(e), nil
	default:
	}

	// Slow path: wait for ctx or acquisition.
	select {
	case e.ch <- struct{}{}:
		return l.makeRelease(e), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// makeRelease returns a one-shot release closure bound to `e`.
func (l *PackageLocker) makeRelease(e *packageLockEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			select {
			case <-e.ch:
			default:
				// Shouldn't happen (lock not held), but avoid panic on
				// double-release or release-without-acquire.
			}
		})
	}
}

// TryAcquire returns (release, true) if the lock is immediately available,
// (nil, false) otherwise. Does not block. Useful for "busy" UI indicators.
func (l *PackageLocker) TryAcquire(source, name string) (func(), bool) {
	l.mu.Lock()
	key := lockKey(source, name)
	e, ok := l.locks[key]
	if !ok {
		e = &packageLockEntry{ch: make(chan struct{}, 1)}
		l.locks[key] = e
	}
	l.mu.Unlock()

	select {
	case e.ch <- struct{}{}:
		return l.makeRelease(e), true
	default:
		return nil, false
	}
}
