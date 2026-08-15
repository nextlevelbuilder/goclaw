package reliability

import (
	"sync"
	"testing"
)

// TestMetricsFlushKeepsPostFlushIncrements verifies the swap-based drain does
// not lose increments that arrive after a flush (regression for the old
// take-then-reset behavior which undercounted at every boundary crossing).
func TestMetricsFlushKeepsPostFlushIncrements(t *testing.T) {
	m := NewMetrics()
	got := Snapshot{}
	RegisterSink(sinkFunc(func(s Snapshot) { got = s }))
	t.Cleanup(func() { RegisterSink(nopSink{}) })

	m.RecordLLMRequest()
	m.Flush()
	if got.LLMRequests != 1 {
		t.Fatalf("first flush emitted %d requests, want 1", got.LLMRequests)
	}

	// An increment landing after the first flush must be emitted by the next.
	m.RecordLLMRequest()
	m.Flush()
	if got.LLMRequests != 1 {
		t.Errorf("post-flush increment lost in second flush: got %d, want 1", got.LLMRequests)
	}
	if m.Take().LLMRequests != 0 {
		t.Errorf("second flush should leave counters drained")
	}
}

// TestConcurrentRecordAndFlush drives a recorder concurrently from several
// goroutines while flushes drain it. Under -race this catches the old
// unsynchronized global-sink write and any counter races. The invariant is
// simply that every flushed plus residual request count equals the total we
// recorded, so no increment is manufactured or irrevocably lost.
func TestConcurrentRecordAndFlush(t *testing.T) {
	m := NewMetrics()
	var mu sync.Mutex
	var emitted uint64
	RegisterSink(sinkFunc(func(s Snapshot) { mu.Lock(); emitted += s.LLMRequests; mu.Unlock() }))
	defer RegisterSink(nopSink{})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				m.RecordLLMRequest()
				if j%25 == 0 {
					m.Flush()
				}
			}
		}()
	}
	wg.Wait()
	m.Flush() // drain anything left

	mu.Lock()
	got := emitted + m.Take().LLMRequests
	mu.Unlock()
	if got != 2000 {
		t.Errorf("emitted+residual = %d, want 2000 recorded requests", got)
	}
}
