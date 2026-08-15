package cache

import (
	"context"
	"time"
)

// Cache is a generic key-value cache interface.
// Implementations: InMemoryCache (this package), future: Redis, Memcache.
type Cache[V any] interface {
	// Get retrieves a value by key. Returns the value and true if found, zero value and false if not.
	Get(ctx context.Context, key string) (V, bool)

	// Set stores a value with an optional TTL. If ttl is 0, the entry never expires.
	Set(ctx context.Context, key string, value V, ttl time.Duration)

	// Delete removes a single key.
	Delete(ctx context.Context, key string)

	// DeleteByPrefix removes all keys matching the given prefix.
	DeleteByPrefix(ctx context.Context, prefix string)

	// Clear removes all entries.
	Clear(ctx context.Context)
}
