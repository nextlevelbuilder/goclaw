package memory

import (
	"strings"
	"sync"
	"time"
)

// L1Cache provides in-memory caching for L1 structured overviews.
// Key bullet points extracted from episodic summaries (~200 tokens each).
type L1Cache struct {
	mu         sync.RWMutex
	entries    map[string]l1Entry
	maxEntries int
	ttl        time.Duration
}

type l1Entry struct {
	content   string
	createdAt time.Time
}

// NewL1Cache creates a cache with given capacity and TTL.
func NewL1Cache(maxEntries int, ttl time.Duration) *L1Cache {
	if maxEntries <= 0 {
		maxEntries = 500
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &L1Cache{entries: make(map[string]l1Entry), maxEntries: maxEntries, ttl: ttl}
}

// Get returns cached L1 content, or empty string if miss/expired.
func (c *L1Cache) Get(id string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[id]
	if !ok || time.Since(e.createdAt) > c.ttl {
		return "", false
	}
	return e.content, true
}

// Put stores L1 content in cache. Evicts oldest entries if full.
func (c *L1Cache) Put(id, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxEntries {
		c.evictOldest(c.maxEntries / 5) // evict 20%
	}
	c.entries[id] = l1Entry{content: content, createdAt: time.Now()}
}

// GenerateL1 creates a key-points summary from a full episodic summary.
// Extractive: takes first 5 non-trivial sentences as bullet points.
func GenerateL1(fullSummary string) string {
	sentences := splitL1Sentences(fullSummary)
	var bullets []string
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) < 20 {
			continue
		}
		bullets = append(bullets, "- "+s)
		if len(bullets) >= 5 {
			break
		}
	}
	return strings.Join(bullets, "\n")
}

func (c *L1Cache) evictOldest(count int) {
	type aged struct {
		key string
		at  time.Time
	}
	var all []aged
	for k, v := range c.entries {
		all = append(all, aged{k, v.createdAt})
	}
	// Simple: delete expired first, then oldest
	evicted := 0
	now := time.Now()
	for _, a := range all {
		if evicted >= count {
			break
		}
		if now.Sub(a.at) > c.ttl {
			delete(c.entries, a.key)
			evicted++
		}
	}
	// If still need to evict, remove oldest by creation time
	for _, a := range all {
		if evicted >= count {
			break
		}
		delete(c.entries, a.key)
		evicted++
	}
}

// splitL1Sentences splits on sentence boundaries for L1 extraction.
func splitL1Sentences(text string) []string {
	// Reuse same logic as consolidation.splitSentences but avoid cross-package dep
	var sentences []string
	var current strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)
		if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) &&
			(runes[i+1] == ' ' || runes[i+1] == '\n') {
			sentences = append(sentences, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}
	return sentences
}
