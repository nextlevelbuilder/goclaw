package oa

import (
	"container/list"
	"sync"
)

// seenMessageIDs is the time==0 dedup fallback for pollOnce. Bounded LRU
// set; usually stays empty since Zalo always sets time in practice.
type seenMessageIDs struct {
	mu    sync.Mutex
	max   int
	data  map[string]*list.Element
	order *list.List
}

func newSeenMessageIDs(max int) *seenMessageIDs {
	if max <= 0 {
		max = 256
	}
	return &seenMessageIDs{
		max:   max,
		data:  make(map[string]*list.Element),
		order: list.New(),
	}
}

// SeenOrAdd reports whether id was already present; otherwise inserts
// as MRU and evicts the LRU tail to keep size <= max.
func (s *seenMessageIDs) SeenOrAdd(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if elem, ok := s.data[id]; ok {
		s.order.MoveToFront(elem)
		return true
	}
	elem := s.order.PushFront(id)
	s.data[id] = elem
	for s.order.Len() > s.max {
		tail := s.order.Back()
		if tail == nil {
			break
		}
		delete(s.data, tail.Value.(string))
		s.order.Remove(tail)
	}
	return false
}
