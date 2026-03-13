package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
)

const groupWriterCacheTTL = 5 * time.Minute

// GroupWriterCache wraps AgentStore.ListGroupFileWriters with a TTL cache.
// Used by tools and agent loop to check group write permissions without repeated DB queries.
type GroupWriterCache struct {
	agentStore AgentStore
	cache      cache.Cache[[]GroupFileWriterData]
}

// NewGroupWriterCache creates a new cache backed by the given agent store.
// The cache implementation is injected (in-memory or Redis) so callers control the backend.
func NewGroupWriterCache(as AgentStore, c cache.Cache[[]GroupFileWriterData]) *GroupWriterCache {
	return &GroupWriterCache{
		agentStore: as,
		cache:      c,
	}
}

func (c *GroupWriterCache) cacheKey(agentID uuid.UUID, groupID string) string {
	return agentID.String() + ":" + groupID
}

// ListWriters returns cached writers, falling back to DB on miss/expiry.
func (c *GroupWriterCache) ListWriters(ctx context.Context, agentID uuid.UUID, groupID string) ([]GroupFileWriterData, error) {
	key := c.cacheKey(agentID, groupID)
	if writers, ok := c.cache.Get(ctx, key); ok {
		return writers, nil
	}
	writers, err := c.agentStore.ListGroupFileWriters(ctx, agentID, groupID)
	if err != nil {
		return nil, err
	}
	c.cache.Set(ctx, key, writers, groupWriterCacheTTL)
	return writers, nil
}

// IsWriter checks if senderNumericID is in the cached writer list.
func (c *GroupWriterCache) IsWriter(ctx context.Context, agentID uuid.UUID, groupID, senderNumericID string) (bool, error) {
	writers, err := c.ListWriters(ctx, agentID, groupID)
	if err != nil {
		return false, err
	}
	for _, w := range writers {
		if w.UserID == senderNumericID {
			return true, nil
		}
	}
	return false, nil
}

// Invalidate clears cache entries matching the given groupID.
func (c *GroupWriterCache) Invalidate(groupID string) {
	// DeleteByPrefix can't match suffix, so we use a sentinel prefix approach:
	// keys are "agentUUID:groupID" — walk via Clear is not viable, so we
	// store a reverse suffix index would add complexity. Instead, clear all
	// and let the next access re-populate. Invalidate is called rarely (writer list changes).
	c.cache.Clear(context.Background())
}

// InvalidateAll clears all cached entries.
func (c *GroupWriterCache) InvalidateAll() {
	c.cache.Clear(context.Background())
}

// CheckGroupWritePermission returns an error if the caller is in a group context
// and is not a file writer. Returns nil if write is allowed.
// Fail-open: returns nil on DB errors or missing context (cron, subagent).
func CheckGroupWritePermission(ctx context.Context, cache *GroupWriterCache) error {
	userID := UserIDFromContext(ctx)
	if !strings.HasPrefix(userID, "group:") && !strings.HasPrefix(userID, "guild:") {
		return nil // not a group context
	}
	agentID := AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil // no agent context
	}
	senderID := SenderIDFromContext(ctx)
	if senderID == "" {
		return nil // system context (cron, subagent)
	}
	numericID := strings.SplitN(senderID, "|", 2)[0]
	isWriter, err := cache.IsWriter(ctx, agentID, userID, numericID)
	if err != nil {
		return nil // fail-open
	}
	if !isWriter {
		return fmt.Errorf("permission denied: only file writers can modify files in this group. Use /addwriter to get write access")
	}
	return nil
}
