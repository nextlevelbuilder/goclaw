package cache

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// entry wraps a cached value with expiration and creation metadata.
// createdAt is used for oldest-first eviction when maxSize is exceeded.
type entry[V any] struct {
	value     V
	expiresAt time.Time // zero means no expiry
	createdAt time.Time // set on Set(), used for eviction ordering
}

func (e entry[V]) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// InMemoryCache is a thread-safe in-memory Cache implementation with TTL support,
// periodic sweep goroutine for expired entries, and optional max-size cap with
// oldest-first eviction.
type InMemoryCache[V any] struct {
	data          sync.Map
	maxSize       int           // 0 = unlimited
	sweepInterval time.Duration // 0 = no periodic sweep (lazy eviction only)
	cancel        context.CancelFunc
	closeOnce     sync.Once
}

// CacheOption configures an InMemoryCache during construction.
type CacheOption[V any] func(*InMemoryCache[V])

// WithMaxSize sets a maximum entry count. When exceeded during sweep, the
// oldest 20% of entries are evicted. Zero = unlimited.
func WithMaxSize[V any](n int) CacheOption[V] {
	return func(c *InMemoryCache[V]) { c.maxSize = n }
}

// WithSweepInterval sets the periodic sweep interval for expired entries.
// Zero disables the sweep goroutine (lazy eviction on Get only).
func WithSweepInterval[V any](d time.Duration) CacheOption[V] {
	return func(c *InMemoryCache[V]) { c.sweepInterval = d }
}

// NewInMemoryCache creates a new in-memory cache. Without options it behaves
// exactly as before (lazy eviction, no size cap, no sweep goroutine).
func NewInMemoryCache[V any](opts ...CacheOption[V]) *InMemoryCache[V] {
	c := &InMemoryCache[V]{}
	for _, opt := range opts {
		opt(c)
	}
	if c.sweepInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		c.cancel = cancel
		go c.sweepLoop(ctx)
	}
	return c
}

func (c *InMemoryCache[V]) Get(_ context.Context, key string) (V, bool) {
	raw, ok := c.data.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	e := raw.(entry[V])
	if e.expired() {
		c.data.Delete(key)
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *InMemoryCache[V]) Set(_ context.Context, key string, value V, ttl time.Duration) {
	e := entry[V]{value: value, createdAt: time.Now()}
	if ttl > 0 {
		e.expiresAt = time.Now().Add(ttl)
	}
	c.data.Store(key, e)
}

func (c *InMemoryCache[V]) Delete(_ context.Context, key string) {
	c.data.Delete(key)
}

func (c *InMemoryCache[V]) DeleteByPrefix(_ context.Context, prefix string) {
	c.data.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			c.data.Delete(key)
		}
		return true
	})
}

func (c *InMemoryCache[V]) Clear(_ context.Context) {
	c.data.Range(func(key, _ any) bool {
		c.data.Delete(key)
		return true
	})
}

// Close stops the background sweep goroutine. Safe to call multiple times.
// After Close, the cache remains readable/writable but without periodic sweep.
func (c *InMemoryCache[V]) Close() {
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})
}

// sweepLoop runs the periodic expiry + size-cap cleanup in a background goroutine.
func (c *InMemoryCache[V]) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(c.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sweepOnce()
		}
	}
}

// sweepOnce deletes expired entries and, if maxSize is set and exceeded,
// evicts the oldest 20% of remaining entries (by createdAt).
func (c *InMemoryCache[V]) sweepOnce() {
	// Phase 1: collect expired keys (don't delete during Range iteration).
	// sync.Map's Range is safe with concurrent Store/Delete, but deleting
	// during iteration can cause the same key to be visited twice in
	// pathological cases. Collecting first is cleaner.
	var expiredKeys []string
	type keyAge struct {
		key       string
		createdAt time.Time
	}
	var allAlive []keyAge

	c.data.Range(func(k, v any) bool {
		key, ok := k.(string)
		if !ok {
			return true
		}
		e, ok := v.(entry[V])
		if !ok {
			return true
		}
		if e.expired() {
			expiredKeys = append(expiredKeys, key)
		} else {
			allAlive = append(allAlive, keyAge{key: key, createdAt: e.createdAt})
		}
		return true
	})

	for _, k := range expiredKeys {
		c.data.Delete(k)
	}

	// Phase 2: size-cap eviction. Sort alive entries by createdAt ascending,
	// evict oldest 20% if over maxSize.
	if c.maxSize > 0 && len(allAlive) > c.maxSize {
		sort.Slice(allAlive, func(i, j int) bool {
			return allAlive[i].createdAt.Before(allAlive[j].createdAt)
		})
		toEvict := len(allAlive) - c.maxSize + (c.maxSize / 5) // bring below cap + 20% headroom
		if toEvict > len(allAlive) {
			toEvict = len(allAlive)
		}
		for i := 0; i < toEvict; i++ {
			c.data.Delete(allAlive[i].key)
		}
	}
}

// sizeLocked returns the current entry count (for tests and metrics).
// Named sizeLocked for historical reasons; sync.Map needs no lock.
func (c *InMemoryCache[V]) sizeLocked() int {
	n := 0
	c.data.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}
