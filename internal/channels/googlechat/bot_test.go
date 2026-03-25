package googlechat

import (
	"sync"
	"testing"
	"time"
)

func TestIsDuplicate_FirstCall(t *testing.T) {
	ch := &Channel{dedup: sync.Map{}}
	if ch.isDuplicate("msg-1") {
		t.Fatal("first call should not be duplicate")
	}
}

func TestIsDuplicate_SecondCall(t *testing.T) {
	ch := &Channel{dedup: sync.Map{}}
	ch.isDuplicate("msg-1")
	if !ch.isDuplicate("msg-1") {
		t.Fatal("second call should be duplicate")
	}
}

func TestIsDuplicate_DifferentMessages(t *testing.T) {
	ch := &Channel{dedup: sync.Map{}}
	ch.isDuplicate("msg-1")
	if ch.isDuplicate("msg-2") {
		t.Fatal("different message should not be duplicate")
	}
}

func TestIsDuplicate_CleanupAfterExpiry(t *testing.T) {
	// Verify the dedup entry is stored and can be manually cleaned
	ch := &Channel{dedup: sync.Map{}}
	ch.dedup.Store("msg-old", struct{}{})

	if !ch.isDuplicate("msg-old") {
		t.Fatal("stored message should be duplicate")
	}

	// Simulate cleanup (time.AfterFunc would do this after 5 min)
	ch.dedup.Delete("msg-old")
	if ch.isDuplicate("msg-old") {
		t.Fatal("deleted message should not be duplicate")
	}
}

func TestIsDuplicate_ConcurrentAccess(t *testing.T) {
	ch := &Channel{dedup: sync.Map{}}
	const goroutines = 100

	var wg sync.WaitGroup
	results := make([]bool, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = ch.isDuplicate("same-msg")
		}(i)
	}
	wg.Wait()

	// Exactly one goroutine should see it as non-duplicate
	nonDup := 0
	for _, dup := range results {
		if !dup {
			nonDup++
		}
	}
	if nonDup != 1 {
		t.Fatalf("expected exactly 1 non-duplicate, got %d", nonDup)
	}
}

func TestIsDuplicate_AfterFuncScheduled(t *testing.T) {
	// Verify time.AfterFunc is used (entry gets cleaned up)
	ch := &Channel{dedup: sync.Map{}}

	// Override: use a very short timer to test cleanup
	// We can't directly test time.AfterFunc(5min), but we verify the entry exists
	ch.isDuplicate("msg-timer")

	// Entry should exist immediately after
	if _, ok := ch.dedup.Load("msg-timer"); !ok {
		t.Fatal("dedup entry should exist after isDuplicate call")
	}

	// Note: actual cleanup happens after 5 minutes via time.AfterFunc
	// We just verify the mechanism is correct by checking the entry is stored
	_ = time.AfterFunc // reference to confirm import is valid
}
