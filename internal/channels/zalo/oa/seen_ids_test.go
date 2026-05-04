package oa

import (
	"fmt"
	"sync"
	"testing"
)

func TestSeenMessageIDs_NotSeenThenSeen(t *testing.T) {
	s := newSeenMessageIDs(8)
	if got := s.SeenOrAdd("m1"); got {
		t.Fatalf("first SeenOrAdd: got true, want false")
	}
	if got := s.SeenOrAdd("m1"); !got {
		t.Fatalf("second SeenOrAdd: got false, want true")
	}
}

func TestSeenMessageIDs_LRUEviction(t *testing.T) {
	s := newSeenMessageIDs(3)
	for _, id := range []string{"a", "b", "c"} {
		if s.SeenOrAdd(id) {
			t.Fatalf("unexpected hit for %q", id)
		}
	}
	// Touch "a" so it's MRU; then push two more — "b" then "c" should evict.
	if !s.SeenOrAdd("a") {
		t.Fatalf("expected hit for a")
	}
	if s.SeenOrAdd("d") {
		t.Fatalf("unexpected hit for d")
	}
	if s.SeenOrAdd("e") {
		t.Fatalf("unexpected hit for e")
	}
	// Final state should be {a, d, e}; b and c evicted.
	if got := s.order.Len(); got != 3 {
		t.Fatalf("len=%d want 3", got)
	}
	for _, id := range []string{"a", "d", "e"} {
		if _, ok := s.data[id]; !ok {
			t.Fatalf("expected %q to be present", id)
		}
	}
	for _, id := range []string{"b", "c"} {
		if _, ok := s.data[id]; ok {
			t.Fatalf("expected %q to be evicted", id)
		}
	}
}

func TestSeenMessageIDs_DefaultMax(t *testing.T) {
	s := newSeenMessageIDs(0) // should clamp to default 256
	for i := range 256 {
		s.SeenOrAdd(fmt.Sprintf("id-%d", i))
	}
	if s.order.Len() != 256 {
		t.Fatalf("len=%d want 256", s.order.Len())
	}
	s.SeenOrAdd("id-256")
	if s.order.Len() != 256 {
		t.Fatalf("len=%d want 256 after overflow", s.order.Len())
	}
}

func TestSeenMessageIDs_ConcurrentSafe(t *testing.T) {
	s := newSeenMessageIDs(1024)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 200 {
				s.SeenOrAdd(fmt.Sprintf("g%d-i%d", g, i))
			}
		}(g)
	}
	wg.Wait()
	if s.order.Len() > 1024 {
		t.Fatalf("len=%d exceeds cap 1024", s.order.Len())
	}
}
