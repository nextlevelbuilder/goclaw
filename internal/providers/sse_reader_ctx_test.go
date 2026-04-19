package providers

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingCloser wraps an io.ReadCloser and counts Close calls.
type countingCloser struct {
	mu        sync.Mutex
	closeCount int
	reader    io.Reader
}

func newCountingCloser(r io.Reader) *countingCloser {
	return &countingCloser{reader: r}
}

func (c *countingCloser) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *countingCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeCount++
	return nil
}

func (c *countingCloser) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCount
}

// blockingReader blocks Read until Close is called (simulates a live TCP socket).
type blockingReader struct {
	ch     chan struct{}
	closed atomic.Bool
}

func newBlockingReader() *blockingReader {
	return &blockingReader{ch: make(chan struct{})}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	<-b.ch
	return 0, errors.New("connection closed")
}

func (b *blockingReader) Close() error {
	if b.closed.CompareAndSwap(false, true) {
		close(b.ch)
	}
	return nil
}

// TestCtxBody_CancelClosesBody verifies that ctx cancellation calls body.Close exactly once
// and that a blocked Read returns an error.
func TestCtxBody_CancelClosesBody(t *testing.T) {
	t.Parallel()

	br := newBlockingReader()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cb := NewCtxBody(ctx, br)

	// Cancel context after a short delay.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	// Read should unblock when Close is called by the watchdog.
	buf := make([]byte, 8)
	_, err := cb.Read(buf)
	if err == nil {
		t.Fatal("expected error from Read after ctx cancel, got nil")
	}

	// Close should be a no-op (already closed by watchdog).
	if err2 := cb.Close(); err2 != nil {
		t.Fatalf("second Close returned error: %v", err2)
	}

	// Underlying body should be closed exactly once.
	if !br.closed.Load() {
		t.Fatal("expected underlying body to be closed")
	}
}

// TestCtxBody_NormalClose_WatchdogExits verifies that calling Close() directly
// releases the watchdog goroutine (no goroutine leak).
func TestCtxBody_NormalClose_WatchdogExits(t *testing.T) {
	t.Parallel()

	before := runtime.NumGoroutine()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cc := newCountingCloser(io.NopCloser(io.LimitReader(nopReader{}, 0)))
			cb := NewCtxBody(ctx, cc)
			// Normal close — watchdog should exit via done channel.
			if err := cb.Close(); err != nil {
				t.Errorf("Close returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Give goroutines time to exit.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()
	// Allow a small tolerance for unrelated goroutines.
	if after > before+5 {
		t.Errorf("goroutine leak: before=%d after=%d (delta=%d)", before, after, after-before)
	}
}

// TestCtxBody_ConcurrentCloseAndCancel verifies no double-close panic under
// concurrent Close() and ctx cancellation. Run with -race flag.
func TestCtxBody_ConcurrentCloseAndCancel(t *testing.T) {
	t.Parallel()

	const iterations = 500
	for range iterations {
		cc := newCountingCloser(io.NopCloser(io.LimitReader(nopReader{}, 0)))
		ctx, cancel := context.WithCancel(context.Background())

		cb := NewCtxBody(ctx, cc)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = cb.Close()
		}()
		go func() {
			defer wg.Done()
			cancel()
		}()
		wg.Wait()

		// Body must be closed exactly once regardless of race.
		if cnt := cc.Count(); cnt != 1 {
			t.Fatalf("expected close count=1, got %d", cnt)
		}
	}
}

// nopReader is an io.Reader that always returns (0, io.EOF).
type nopReader struct{}

func (nopReader) Read([]byte) (int, error) { return 0, io.EOF }
