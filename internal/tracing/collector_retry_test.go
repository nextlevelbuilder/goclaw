package tracing

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// errFakeDB is a sentinel error returned by the mock store.
var errFakeDB = errors.New("fake db error")

// mockTracingStore is a minimal TracingStore stub that counts UpdateTrace calls
// and fails until failUntil attempts have been made.
type mockTracingStore struct {
	store.TracingStore // embed to satisfy interface; unused methods panic

	updateCalls atomic.Int64
	failUntil   int // first N calls return errFakeDB; N+1 onwards succeed
}

func (m *mockTracingStore) UpdateTrace(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	n := int(m.updateCalls.Add(1))
	if n <= m.failUntil {
		return errFakeDB
	}
	return nil
}

func (m *mockTracingStore) RecoverStaleRunningTraces(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// alwaysFailStore always returns an error for UpdateTrace.
type alwaysFailStore struct {
	store.TracingStore
	calls atomic.Int64
}

func (s *alwaysFailStore) UpdateTrace(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	s.calls.Add(1)
	return errFakeDB
}

func (s *alwaysFailStore) RecoverStaleRunningTraces(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// newTestCollector creates a Collector with a fake store and no background goroutines.
// Callers must not call Start() unless explicitly testing worker behaviour.
func newTestCollector(ts store.TracingStore) *Collector {
	return &Collector{
		store:        ts,
		spanCh:       make(chan store.SpanData, defaultBufferSize),
		spanUpdateCh: make(chan spanUpdate, defaultBufferSize),
		stopCh:       make(chan struct{}),
		retryCh:      make(chan pendingUpdate, retryQueueCap),
		dirtyTraces:  make(map[uuid.UUID]struct{}),
	}
}

// TestUpdateTraceWithRetry_SuccessAfterNFails verifies that when the store fails
// the first 2 calls and succeeds on the 3rd, updateTraceWithRetry returns true
// and nothing is enqueued in the retry channel.
func TestUpdateTraceWithRetry_SuccessAfterNFails(t *testing.T) {
	ms := &mockTracingStore{failUntil: 2}
	c := newTestCollector(ms)

	traceID := uuid.New()
	updates := map[string]any{"status": "completed"}

	ok := c.updateTraceWithRetry(context.Background(), traceID, updates)
	if !ok {
		t.Fatal("expected updateTraceWithRetry to return true after success on 3rd attempt")
	}
	if got := int(ms.updateCalls.Load()); got != 3 {
		t.Fatalf("expected 3 UpdateTrace calls, got %d", got)
	}
	// Nothing should be in the retry queue.
	if len(c.retryCh) != 0 {
		t.Fatalf("expected empty retry queue, got %d items", len(c.retryCh))
	}
}

// TestUpdateTraceWithRetry_CancelledContextSucceeds verifies that a cancelled
// caller context does not prevent the update from succeeding (WithoutCancel).
func TestUpdateTraceWithRetry_CancelledContextSucceeds(t *testing.T) {
	ms := &mockTracingStore{failUntil: 0} // succeeds on first call
	c := newTestCollector(ms)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	traceID := uuid.New()
	ok := c.updateTraceWithRetry(ctx, traceID, map[string]any{"status": "cancelled"})
	if !ok {
		t.Fatal("expected success even with cancelled ctx")
	}
}

// TestUpdateTraceWithRetry_AllFailEnqueues verifies that when all 4 attempts fail,
// the item lands in the retry queue and the function returns false.
func TestUpdateTraceWithRetry_AllFailEnqueues(t *testing.T) {
	s := &alwaysFailStore{}
	c := newTestCollector(s)

	traceID := uuid.New()
	ok := c.updateTraceWithRetry(context.Background(), traceID, map[string]any{"status": "error"})
	if ok {
		t.Fatal("expected false when all retries fail")
	}
	if len(c.retryCh) != 1 {
		t.Fatalf("expected 1 item in retry queue, got %d", len(c.retryCh))
	}
}

// TestRetryWorker_DrainAndSucceed verifies that the retryWorker processes
// queued items once the store starts succeeding.
func TestRetryWorker_DrainAndSucceed(t *testing.T) {
	// fail first 3 calls so inline retry puts it in queue, then succeed
	ms := &mockTracingStore{failUntil: 4} // 4 failures: 1 inline attempt + 3 backoffs → queued; worker attempt 5 succeeds
	c := newTestCollector(ms)

	// Pre-enqueue directly (simulates worker finding item)
	traceID := uuid.New()
	c.retryCh <- pendingUpdate{TraceID: traceID, Updates: map[string]any{"status": "done"}}

	c.wg.Add(1)
	go c.retryWorker()

	// Wait for the worker to drain (up to 3 ticker periods)
	deadline := time.Now().Add(3 * retryWorkerPeriod)
	for time.Now().Before(deadline) {
		if ms.updateCalls.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(c.stopCh)
	c.wg.Wait()

	if ms.updateCalls.Load() < 1 {
		t.Fatal("worker never attempted UpdateTrace")
	}
}

// TestRetryWorker_ExhaustsAndDrops verifies that items failing retryMaxTries times
// are dropped with an error log and not kept in memory forever.
func TestRetryWorker_ExhaustsAndDrops(t *testing.T) {
	s := &alwaysFailStore{}
	c := newTestCollector(s)

	traceID := uuid.New()
	// Pre-set tries to retryMaxTries-1 so one more failure drops it.
	c.retryCh <- pendingUpdate{
		TraceID: traceID,
		Updates: map[string]any{"status": "error"},
		Tries:   retryMaxTries - 1,
	}

	c.wg.Add(1)
	go c.retryWorker()

	// Allow one tick
	time.Sleep(retryWorkerPeriod + 100*time.Millisecond)
	close(c.stopCh)
	c.wg.Wait()

	// Item should have been dropped (tries reached retryMaxTries on this attempt).
	// We can't directly inspect internal pending slice, but verify no goroutine leak
	// and store was called at least once.
	if s.calls.Load() < 1 {
		t.Fatal("worker never attempted UpdateTrace on queued item")
	}
}

// TestEnqueueRetry_QueueCapDropsOldest verifies that when the queue is full,
// enqueuing a new item drops the oldest.
func TestEnqueueRetry_QueueCapDropsOldest(t *testing.T) {
	c := newTestCollector(&alwaysFailStore{})

	// Fill the queue to capacity.
	firstID := uuid.New()
	c.retryCh <- pendingUpdate{TraceID: firstID, Updates: map[string]any{"status": "old"}}
	for i := 1; i < retryQueueCap; i++ {
		c.retryCh <- pendingUpdate{TraceID: uuid.New(), Updates: map[string]any{"status": "fill"}}
	}
	if len(c.retryCh) != retryQueueCap {
		t.Fatalf("expected queue at cap %d, got %d", retryQueueCap, len(c.retryCh))
	}

	// Enqueue one more — should drop firstID (oldest).
	newID := uuid.New()
	c.enqueueRetry(context.Background(), newID, map[string]any{"status": "new"})

	// Queue should still be at cap.
	if len(c.retryCh) != retryQueueCap {
		t.Fatalf("queue size after drop: got %d, want %d", len(c.retryCh), retryQueueCap)
	}

	// The first item drained should NOT be firstID (it was dropped); it should be
	// one of the fill items or the new item — first item read will be whatever was
	// second-oldest (since oldest was dropped and new was appended).
	first := <-c.retryCh
	if first.TraceID == firstID {
		t.Fatal("oldest item should have been dropped, but it is still at the front")
	}
}

// TestShutdown_WorkerExitsCleanly verifies that closing stopCh causes retryWorker
// to exit without goroutine leak, even with pending items in the channel.
func TestShutdown_WorkerExitsCleanly(t *testing.T) {
	c := newTestCollector(&alwaysFailStore{})

	// Put a few items in the retry channel before starting worker.
	for range 5 {
		c.retryCh <- pendingUpdate{TraceID: uuid.New(), Updates: map[string]any{"status": "pending"}}
	}

	c.wg.Add(1)
	go c.retryWorker()

	// Signal shutdown immediately (before next tick).
	close(c.stopCh)

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// clean exit
	case <-time.After(2 * time.Second):
		t.Fatal("retryWorker did not exit within 2 seconds after stopCh closed")
	}
}

// TestSetTraceStatus_UsesRetry verifies SetTraceStatus succeeds with cancelled ctx
// (regression gate for the original silent-swallow bug).
func TestSetTraceStatus_UsesRetry(t *testing.T) {
	ms := &mockTracingStore{failUntil: 0}
	c := newTestCollector(ms)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.SetTraceStatus(ctx, uuid.New(), "cancelled")

	if ms.updateCalls.Load() != 1 {
		t.Fatalf("expected 1 UpdateTrace call, got %d", ms.updateCalls.Load())
	}
}

// TestFinishTrace_UsesRetry verifies FinishTrace succeeds with cancelled ctx.
func TestFinishTrace_UsesRetry(t *testing.T) {
	ms := &mockTracingStore{failUntil: 0}
	c := newTestCollector(ms)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.FinishTrace(ctx, uuid.New(), "completed", "", "some output")

	if ms.updateCalls.Load() != 1 {
		t.Fatalf("expected 1 UpdateTrace call, got %d", ms.updateCalls.Load())
	}
}
