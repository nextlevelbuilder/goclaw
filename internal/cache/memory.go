package cache

import (
	"context"
	"strings"
	"sync"
	"time"
)

// entry wraps a cached value with expiration metadata.
type entry[V any] struct {
	value     V
	expiresAt time.Time // zero means no expiry
}

func (e entry[V]) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// InMemoryCache is a thread-safe in-memory Cache implementation with TTL support.
type InMemoryCache[V any] struct {
	data sync.Map
}

// NewInMemoryCache creates a new in-memory cache.
func NewInMemoryCache[V any]() *InMemoryCache[V] {
	return &InMemoryCache[V]{}
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
	e := entry[V]{value: value}
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
