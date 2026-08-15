//go:build redis

package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache is a Cache implementation backed by Redis.
// All Redis errors are treated as cache misses (fail-open) to avoid breaking callers.
type RedisCache[V any] struct {
	client *redis.Client
	prefix string // key namespace, e.g. "ctx:agent"
}

// NewRedisCache creates a Redis-backed cache with the given key prefix.
// Keys are stored as "goclaw:{prefix}:{key}".
func NewRedisCache[V any](client *redis.Client, prefix string) *RedisCache[V] {
	return &RedisCache[V]{client: client, prefix: prefix}
}

func (c *RedisCache[V]) fullKey(key string) string {
	return "goclaw:" + c.prefix + ":" + key
}

func (c *RedisCache[V]) keyPattern() string {
	return "goclaw:" + c.prefix + ":*"
}

func (c *RedisCache[V]) Get(ctx context.Context, key string) (V, bool) {
	var zero V
	data, err := c.client.Get(ctx, c.fullKey(key)).Bytes()
	if err != nil {
		return zero, false
	}
	var val V
	if err := json.Unmarshal(data, &val); err != nil {
		slog.Warn("redis cache: unmarshal error", "key", c.fullKey(key), "error", err)
		return zero, false
	}
	return val, true
}

func (c *RedisCache[V]) Set(ctx context.Context, key string, value V, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		slog.Warn("redis cache: marshal error", "key", c.fullKey(key), "error", err)
		return
	}
	if err := c.client.Set(ctx, c.fullKey(key), data, ttl).Err(); err != nil {
		slog.Warn("redis cache: set error", "key", c.fullKey(key), "error", err)
	}
}

func (c *RedisCache[V]) Delete(ctx context.Context, key string) {
	if err := c.client.Del(ctx, c.fullKey(key)).Err(); err != nil {
		slog.Warn("redis cache: delete error", "key", c.fullKey(key), "error", err)
	}
}

func (c *RedisCache[V]) DeleteByPrefix(ctx context.Context, prefix string) {
	pattern := "goclaw:" + c.prefix + ":" + prefix + "*"
	c.deleteByPattern(ctx, pattern)
}

func (c *RedisCache[V]) Clear(ctx context.Context) {
	c.deleteByPattern(ctx, c.keyPattern())
}

// deleteByPattern scans for keys matching pattern and deletes them in batches.
func (c *RedisCache[V]) deleteByPattern(ctx context.Context, pattern string) {
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			slog.Warn("redis cache: scan error", "pattern", pattern, "error", err)
			return
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				slog.Warn("redis cache: batch delete error", "count", len(keys), "error", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}
