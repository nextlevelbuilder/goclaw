package telegram

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mymmrac/telego"
)

// rctxZero returns a zero-valued resolvedMessageContext for tests that don't
// care about downstream dispatch (aggregator unit tests).
func rctxZero() resolvedMessageContext { return resolvedMessageContext{} }

// mkAlbumMsg builds a minimal *telego.Message for aggregator tests.
func mkAlbumMsg(chatID, userID int64, groupID string, msgID int) *telego.Message {
	return &telego.Message{
		MessageID:    msgID,
		Chat:         telego.Chat{ID: chatID},
		From:         &telego.User{ID: userID},
		MediaGroupID: groupID,
	}
}

type capturedFlushes struct {
	mu      sync.Mutex
	batches [][]*telego.Message
	done    chan struct{}
}

func newCapturedFlushes() *capturedFlushes {
	return &capturedFlushes{done: make(chan struct{}, 64)}
}

func (c *capturedFlushes) callback() func(resolvedMessageContext, []*telego.Message) {
	return func(_ resolvedMessageContext, members []*telego.Message) {
		c.mu.Lock()
		batch := append([]*telego.Message(nil), members...)
		c.batches = append(c.batches, batch)
		c.mu.Unlock()
		select {
		case c.done <- struct{}{}:
		default:
		}
	}
}

func (c *capturedFlushes) batchCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.batches)
}

func (c *capturedFlushes) batch(i int) []*telego.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.batches) {
		return nil
	}
	return c.batches[i]
}

func waitFlush(t *testing.T, c *capturedFlushes, timeout time.Duration) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for album flush after %s", timeout)
	}
}

func TestAlbumAggregator_FlushesOnSilence(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(50*time.Millisecond, 100, 1000, cap.callback())
	defer a.Stop()

	if !a.Push(mkAlbumMsg(1, 10, "g1", 100), rctxZero()) {
		t.Fatal("push 1 rejected")
	}
	time.Sleep(15 * time.Millisecond)
	a.Push(mkAlbumMsg(1, 10, "g1", 101), rctxZero())
	time.Sleep(15 * time.Millisecond)
	a.Push(mkAlbumMsg(1, 10, "g1", 102), rctxZero())

	waitFlush(t, cap, 500*time.Millisecond)
	time.Sleep(80 * time.Millisecond)

	if cap.batchCount() != 1 {
		t.Fatalf("flushes=%d, want 1", cap.batchCount())
	}
	got := cap.batch(0)
	if len(got) != 3 {
		t.Fatalf("members=%d, want 3", len(got))
	}
	if got[0].MessageID != 100 || got[1].MessageID != 101 || got[2].MessageID != 102 {
		t.Fatalf("arrival order broken: %d %d %d", got[0].MessageID, got[1].MessageID, got[2].MessageID)
	}
}

func TestAlbumAggregator_DistinctMediaGroupsAreSeparateBuffers(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(40*time.Millisecond, 100, 1000, cap.callback())
	defer a.Stop()

	a.Push(mkAlbumMsg(1, 10, "g1", 100), rctxZero())
	a.Push(mkAlbumMsg(1, 20, "g2", 200), rctxZero())

	waitFlush(t, cap, 500*time.Millisecond)
	waitFlush(t, cap, 500*time.Millisecond)
	time.Sleep(60 * time.Millisecond)

	if cap.batchCount() != 2 {
		t.Fatalf("flushes=%d, want 2 (one per MediaGroupID)", cap.batchCount())
	}
}

func TestAlbumAggregator_SenderRebindDropsAndWarns(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(40*time.Millisecond, 100, 1000, cap.callback())
	defer a.Stop()

	if !a.Push(mkAlbumMsg(1, 10, "g1", 100), rctxZero()) {
		t.Fatal("first push should succeed")
	}
	if a.Push(mkAlbumMsg(1, 999, "g1", 101), rctxZero()) {
		t.Fatal("sender-rebind push should be rejected")
	}

	waitFlush(t, cap, 500*time.Millisecond)
	time.Sleep(60 * time.Millisecond)

	if cap.batchCount() != 1 {
		t.Fatalf("flushes=%d, want 1", cap.batchCount())
	}
	got := cap.batch(0)
	if len(got) != 1 || got[0].MessageID != 100 {
		t.Fatalf("buffer contaminated by rebind: %v", got)
	}
}

func TestAlbumAggregator_ResetsOnNewArrival(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(80*time.Millisecond, 100, 1000, cap.callback())
	defer a.Stop()

	for i := 0; i < 4; i++ {
		a.Push(mkAlbumMsg(1, 10, "g1", 100+i), rctxZero())
		time.Sleep(50 * time.Millisecond)
	}

	waitFlush(t, cap, 500*time.Millisecond)
	time.Sleep(120 * time.Millisecond)

	if cap.batchCount() != 1 {
		t.Fatalf("flushes=%d, want 1 (timer should reset on each push)", cap.batchCount())
	}
	got := cap.batch(0)
	if len(got) != 4 {
		t.Fatalf("members=%d, want 4", len(got))
	}
}

func TestAlbumAggregator_StopFlushesPendingImmediately(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(time.Minute, 100, 1000, cap.callback())

	a.Push(mkAlbumMsg(1, 10, "g1", 100), rctxZero())
	a.Push(mkAlbumMsg(1, 10, "g1", 101), rctxZero())

	a.Stop()

	if cap.batchCount() != 1 {
		t.Fatalf("flushes=%d, want 1 (Stop must flush pending)", cap.batchCount())
	}
	if got := cap.batch(0); len(got) != 2 {
		t.Fatalf("members=%d, want 2", len(got))
	}
}

func TestAlbumAggregator_RespectsPerBufferCap(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(time.Minute, 5, 1000, cap.callback())
	defer a.Stop()

	for i := 0; i < 5; i++ {
		if !a.Push(mkAlbumMsg(1, 10, "g1", 100+i), rctxZero()) {
			t.Fatalf("push %d rejected before cap reached", i)
		}
	}
	if a.Push(mkAlbumMsg(1, 10, "g1", 999), rctxZero()) {
		t.Fatal("push past per-buffer cap should be rejected")
	}
	if cap.batchCount() != 0 {
		t.Fatalf("flushes=%d, want 0 (cap must not cause early flush)", cap.batchCount())
	}
}

func TestAlbumAggregator_RespectsGlobalBufferCap(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(time.Minute, 100, 3, cap.callback())
	defer a.Stop()

	for i := 0; i < 3; i++ {
		if !a.Push(mkAlbumMsg(int64(i+1), 10, fmt.Sprintf("g%d", i), 100), rctxZero()) {
			t.Fatalf("push %d rejected before global cap reached", i)
		}
	}
	if a.Push(mkAlbumMsg(99, 10, "g99", 100), rctxZero()) {
		t.Fatal("push past global buffer cap should be rejected")
	}
}

func TestAlbumAggregator_EmptyMediaGroupIDRejected(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(40*time.Millisecond, 100, 1000, cap.callback())
	defer a.Stop()

	msg := mkAlbumMsg(1, 10, "", 100)
	if a.Push(msg, rctxZero()) {
		t.Fatal("Push with empty MediaGroupID must return false (caller must gate)")
	}
}

func TestAlbumAggregator_PostStopPushIgnored(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(40*time.Millisecond, 100, 1000, cap.callback())

	a.Stop()
	if a.Push(mkAlbumMsg(1, 10, "g1", 100), rctxZero()) {
		t.Fatal("Push after Stop must be rejected")
	}
}

func TestAlbumAggregator_TimerNoLeakAfterFlush(t *testing.T) {
	cap := newCapturedFlushes()
	a := newAlbumAggregator(30*time.Millisecond, 100, 1000, cap.callback())
	defer a.Stop()

	var firedFlushes int32
	originalFlush := a.flushFn
	a.flushFn = func(rctx resolvedMessageContext, members []*telego.Message) {
		atomic.AddInt32(&firedFlushes, 1)
		originalFlush(rctx, members)
	}

	a.Push(mkAlbumMsg(1, 10, "g1", 100), rctxZero())

	waitFlush(t, cap, 500*time.Millisecond)
	time.Sleep(120 * time.Millisecond)

	a.mu.Lock()
	bufCount := len(a.buffers)
	a.mu.Unlock()
	if bufCount != 0 {
		t.Fatalf("buffer not released after flush: %d remaining", bufCount)
	}
	if got := atomic.LoadInt32(&firedFlushes); got != 1 {
		t.Fatalf("flushFn fired %d times, want 1", got)
	}
}
