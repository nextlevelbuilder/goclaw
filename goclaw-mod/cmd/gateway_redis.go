//go:build redis

package cmd

import (
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// initRedisClient creates a Redis client when built with -tags redis.
// Returns nil (typed as any) if GOCLAW_REDIS_DSN is empty or connection fails.
func initRedisClient(cfg *config.Config) any {
	dsn := cfg.Database.RedisDSN
	if dsn == "" {
		slog.Debug("Redis available but not configured (set GOCLAW_REDIS_DSN)")
		return nil
	}
	client, err := cache.NewRedisClient(dsn)
	if err != nil {
		slog.Warn("Redis connection failed, falling back to in-memory cache", "error", err)
		return nil
	}
	slog.Info("Redis cache connected")
	return client
}

// makeCaches creates typed cache instances backed by Redis (or in-memory if client is nil).
func makeCaches(raw any) (
	agentCtxCache cache.Cache[[]store.AgentContextFileData],
	userCtxCache cache.Cache[[]store.AgentContextFileData],
) {
	client, _ := raw.(*redis.Client)
	if client == nil {
		slog.Info("cache backend: in-memory (Redis not connected)")
		return cache.NewInMemoryCache[[]store.AgentContextFileData](),
			cache.NewInMemoryCache[[]store.AgentContextFileData]()
	}
	slog.Info("cache backend: redis")
	return cache.NewRedisCache[[]store.AgentContextFileData](client, "ctx:agent"),
		cache.NewRedisCache[[]store.AgentContextFileData](client, "ctx:user")
}

// shutdownRedis closes the Redis client connection.
func shutdownRedis(raw any) {
	if client, ok := raw.(*redis.Client); ok && client != nil {
		client.Close()
	}
}
