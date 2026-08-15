//go:build !redis

package cmd

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// initRedisClient is a no-op when built without the "redis" tag.
// Build with `go build -tags redis` to enable Redis cache.
func initRedisClient(_ *config.Config) any { return nil }

// makeCaches returns in-memory cache instances when Redis is not compiled in.
func makeCaches(_ any) (
	agentCtxCache cache.Cache[[]store.AgentContextFileData],
	userCtxCache cache.Cache[[]store.AgentContextFileData],
) {
	slog.Info("cache backend: in-memory")
	return cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData]()
}

// shutdownRedis is a no-op when built without the "redis" tag.
func shutdownRedis(_ any) {}
