package oa

import (
	"container/list"
	"encoding/json"
	"sort"
	"sync"
)

const (
	defaultCursorMaxEntries = 500
	configCursorKey         = "poll_cursor"
)

// pollCursor tracks last-seen unix-ms per user_id to dedup polling.
// Bounded LRU; evicted users may re-receive a single message next time.
type pollCursor struct {
	mu    sync.Mutex
	max   int
	data  map[string]*list.Element // user_id → element holding cursorEntry
	order *list.List               // front = most-recently-used
	dirty bool
}

type cursorEntry struct {
	userID string
	ts     int64
}

func newPollCursor(max int) *pollCursor {
	if max <= 0 {
		max = defaultCursorMaxEntries
	}
	return &pollCursor{
		max:   max,
		data:  make(map[string]*list.Element),
		order: list.New(),
	}
}

// Advance sets the cursor for userID if ts is strictly newer. Always
// promotes to MRU. Returns true if the cursor moved.
func (c *pollCursor) Advance(userID string, ts int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.data[userID]; ok {
		entry := elem.Value.(*cursorEntry)
		if ts <= entry.ts {
			c.order.MoveToFront(elem)
			return false
		}
		entry.ts = ts
		c.order.MoveToFront(elem)
		c.dirty = true
		return true
	}
	entry := &cursorEntry{userID: userID, ts: ts}
	elem := c.order.PushFront(entry)
	c.data[userID] = elem
	c.dirty = true
	c.evictLocked()
	return true
}

func (c *pollCursor) Get(userID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.data[userID]; ok {
		return elem.Value.(*cursorEntry).ts
	}
	return 0
}

// LastSeenTimestamp returns the max unix-ms across all entries (0 if empty).
func (c *pollCursor) LastSeenTimestamp() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var max int64
	for _, elem := range c.data {
		if ts := elem.Value.(*cursorEntry).ts; ts > max {
			max = ts
		}
	}
	return max
}

// Snapshot returns a mutable copy of the cursor map.
func (c *pollCursor) Snapshot() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int64, len(c.data))
	for k, elem := range c.data {
		out[k] = elem.Value.(*cursorEntry).ts
	}
	return out
}

func (c *pollCursor) IsDirty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dirty
}

func (c *pollCursor) ClearDirty() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dirty = false
}

// evictLocked drops the LRU tail until size <= max. Holds mu.
func (c *pollCursor) evictLocked() {
	for c.order.Len() > c.max {
		tail := c.order.Back()
		if tail == nil {
			return
		}
		entry := tail.Value.(*cursorEntry)
		delete(c.data, entry.userID)
		c.order.Remove(tail)
	}
}

// loadFromMap seeds the cursor. Sorts by timestamp ascending so eviction
// on overflow drops the oldest cursors deterministically.
func (c *pollCursor) loadFromMap(m map[string]int64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] < m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		c.Advance(k, m[k])
	}
	c.ClearDirty()
}

// parseCursorFromConfig extracts the poll_cursor sub-object from the
// channel_instances.config blob (empty map on missing/invalid).
func parseCursorFromConfig(raw []byte) map[string]int64 {
	out := map[string]int64{}
	if len(raw) == 0 {
		return out
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return out
	}
	cursorRaw, ok := top[configCursorKey]
	if !ok {
		return out
	}
	_ = json.Unmarshal(cursorRaw, &out)
	return out
}

