package channels

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// noopPendingStore is the smallest PendingMessageStore that makes a
// PendingHistory persistent (store != nil) without touching a database.
type noopPendingStore struct{}

func (noopPendingStore) AppendBatch(context.Context, []store.PendingMessage) error { return nil }
func (noopPendingStore) ListByKey(context.Context, string, string) ([]store.PendingMessage, error) {
	return nil, nil
}
func (noopPendingStore) DeleteByKey(context.Context, string, string) error { return nil }
func (noopPendingStore) Compact(context.Context, []uuid.UUID, *store.PendingMessage) error {
	return nil
}
func (noopPendingStore) DeleteStale(context.Context, time.Duration) (int64, error) { return 0, nil }
func (noopPendingStore) ListArchivedByKey(context.Context, string, string, time.Time, int) ([]store.ArchivedMessage, error) {
	return nil, nil
}
func (noopPendingStore) ListGroups(context.Context) ([]store.PendingMessageGroup, error) {
	return nil, nil
}
func (noopPendingStore) CountAll(context.Context) (int64, error)                 { return 0, nil }
func (noopPendingStore) CountByKey(context.Context, string, string) (int, error) { return 0, nil }
func (noopPendingStore) ResolveGroupTitles(context.Context, []store.PendingMessageGroup) (map[string]string, error) {
	return nil, nil
}

// stopFlusherReturns runs StopFlusher and fails the test if it does not come
// back within the deadline — the pre-fix behaviour is to block forever on
// <-ph.stopped when flushLoop was never started.
func stopFlusherReturns(t *testing.T, ph *PendingHistory, within time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		ph.StopFlusher()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("StopFlusher did not return within %s", within)
	}
}

// TestStopFlusherWithoutStartReturns covers the production hang: a channel
// whose Start() failed before it reached StartFlusher() is still Stop()ped by
// InstanceLoader.Reload, and Stop() calls StopFlusher() on a persistent
// history whose flush goroutine never existed.
func TestStopFlusherWithoutStartReturns(t *testing.T) {
	ph := NewPersistentHistory("telegram-test", noopPendingStore{}, uuid.Nil)

	stopFlusherReturns(t, ph, time.Second)
}

// TestStopFlusherTwiceDoesNotPanic: Stop() may run more than once on the same
// channel (Reload after a timed-out Start already stopped it). A second
// close(stopCh) would panic; the second call must be a no-op instead.
func TestStopFlusherTwiceDoesNotPanic(t *testing.T) {
	ph := NewPersistentHistory("telegram-test", noopPendingStore{}, uuid.Nil)
	ph.StartFlusher()

	stopFlusherReturns(t, ph, time.Second)
	stopFlusherReturns(t, ph, time.Second)
}

// TestStartFlusherTwiceStartsOneLoop: a repeated StartFlusher() must not spawn
// a second flushLoop — two loops would race on close(ph.stopped).
func TestStartFlusherTwiceStartsOneLoop(t *testing.T) {
	ph := NewPersistentHistory("telegram-test", noopPendingStore{}, uuid.Nil)
	ph.StartFlusher()
	ph.StartFlusher()

	// With two loops, the second close(stopped) panics inside a goroutine
	// and takes the test binary down; with one loop this just returns.
	stopFlusherReturns(t, ph, time.Second)
	time.Sleep(50 * time.Millisecond) // give a stray second loop time to crash the binary
}
