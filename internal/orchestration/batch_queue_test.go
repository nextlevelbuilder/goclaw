package orchestration

import (
	"sync"
	"testing"
)

func TestBatchQueue_FirstEnqueue_IsProcessor(t *testing.T) {
	var bq BatchQueue[string]
	if !bq.Enqueue("k1", "a") {
		t.Error("first enqueue should return isProcessor=true")
	}
}

func TestBatchQueue_SecondEnqueue_NotProcessor(t *testing.T) {
	var bq BatchQueue[string]
	bq.Enqueue("k1", "a")
	if bq.Enqueue("k1", "b") {
		t.Error("second enqueue should return isProcessor=false")
	}
}

func TestBatchQueue_Drain_ReturnsAll(t *testing.T) {
	var bq BatchQueue[string]
	bq.Enqueue("k1", "a")
	bq.Enqueue("k1", "b")
	entries := bq.Drain("k1")
	if len(entries) != 2 {
		t.Fatalf("drain returned %d entries, want 2", len(entries))
	}
	if entries[0] != "a" || entries[1] != "b" {
		t.Errorf("entries = %v", entries)
	}
	// Second drain should be empty
	if got := bq.Drain("k1"); len(got) != 0 {
		t.Errorf("second drain should be empty, got %d", len(got))
	}
}

func TestBatchQueue_TryFinish_EmptyReturnsTrue(t *testing.T) {
	var bq BatchQueue[string]
	bq.Enqueue("k1", "a")
	bq.Drain("k1")
	if !bq.TryFinish("k1") {
		t.Error("tryFinish on empty queue should return true")
	}
}

func TestBatchQueue_TryFinish_PendingReturnsFalse(t *testing.T) {
	var bq BatchQueue[string]
	bq.Enqueue("k1", "a")
	bq.Drain("k1")
	bq.Enqueue("k1", "b") // new entry arrives
	if bq.TryFinish("k1") {
		t.Error("tryFinish with pending entries should return false")
	}
}

func TestBatchQueue_AfterFinish_NewEnqueueIsProcessor(t *testing.T) {
	var bq BatchQueue[string]
	bq.Enqueue("k1", "a")
	bq.Drain("k1")
	bq.TryFinish("k1")
	// Queue cleaned up — next enqueue should be processor again
	if !bq.Enqueue("k1", "b") {
		t.Error("enqueue after finish should return isProcessor=true")
	}
}

func TestBatchQueue_SeparateKeys(t *testing.T) {
	var bq BatchQueue[string]
	bq.Enqueue("k1", "a")
	bq.Enqueue("k2", "x")
	e1 := bq.Drain("k1")
	e2 := bq.Drain("k2")
	if len(e1) != 1 || e1[0] != "a" {
		t.Errorf("k1 = %v", e1)
	}
	if len(e2) != 1 || e2[0] != "x" {
		t.Errorf("k2 = %v", e2)
	}
}

func TestBatchQueue_ConcurrentEnqueue(t *testing.T) {
	var bq BatchQueue[int]
	const n = 100
	var wg sync.WaitGroup
	processors := 0
	var mu sync.Mutex

	for i := range n {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			if bq.Enqueue("key", v) {
				mu.Lock()
				processors++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if processors != 1 {
		t.Errorf("expected exactly 1 processor, got %d", processors)
	}
	entries := bq.Drain("key")
	if len(entries) != n {
		t.Errorf("expected %d entries, got %d", n, len(entries))
	}
}

func TestBatchQueue_DrainUnknownKey(t *testing.T) {
	var bq BatchQueue[string]
	if got := bq.Drain("nonexistent"); got != nil {
		t.Errorf("drain unknown key should return nil, got %v", got)
	}
}

func TestBatchQueue_TryFinishUnknownKey(t *testing.T) {
	var bq BatchQueue[string]
	if !bq.TryFinish("nonexistent") {
		t.Error("tryFinish unknown key should return true")
	}
}
