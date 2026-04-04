package browser

import (
	"regexp"
	"strings"
	"sync"
)

const defaultMaxRefStoreSize = 50

var refPattern = regexp.MustCompile(`^e\d+$`)

// RefStore is a thread-safe, per-tab LRU cache for snapshot refs.
// Each entry maps a targetID to its ref→RoleRef mapping from the last snapshot.
type RefStore struct {
	mu      sync.RWMutex
	entries map[string]map[string]RoleRef
	order   []string // LRU order (most recently used at end)
	maxSize int
}

// NewRefStore creates a RefStore with default capacity.
func NewRefStore() *RefStore {
	return &RefStore{
		entries: make(map[string]map[string]RoleRef),
		maxSize: defaultMaxRefStoreSize,
	}
}

// Store saves refs for a target, evicting oldest entries if over capacity.
func (rs *RefStore) Store(targetID string, refs map[string]RoleRef) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Remove from current position in LRU order
	rs.removeFromOrder(targetID)

	// Add to end (most recently used)
	rs.order = append(rs.order, targetID)
	rs.entries[targetID] = refs

	// Evict oldest if over capacity
	for len(rs.order) > rs.maxSize {
		oldest := rs.order[0]
		rs.order = rs.order[1:]
		delete(rs.entries, oldest)
	}
}

// Resolve looks up a ref for a given target.
func (rs *RefStore) Resolve(targetID, ref string) (*RoleRef, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	normalized := NormalizeRef(ref)
	refs, ok := rs.entries[targetID]
	if !ok {
		return nil, false
	}
	r, ok := refs[normalized]
	if !ok {
		return nil, false
	}
	return &r, true
}

// NormalizeRef normalizes ref formats: "@e5", "ref=e5", "e5" → "e5".
func NormalizeRef(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "@") {
		s = s[1:]
	} else if strings.HasPrefix(s, "ref=") {
		s = s[4:]
	}
	if refPattern.MatchString(s) {
		return s
	}
	return s
}

// Remove deletes all refs for a target.
func (rs *RefStore) Remove(targetID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.removeFromOrder(targetID)
	delete(rs.entries, targetID)
}

func (rs *RefStore) removeFromOrder(targetID string) {
	for i, id := range rs.order {
		if id == targetID {
			rs.order = append(rs.order[:i], rs.order[i+1:]...)
			return
		}
	}
}
