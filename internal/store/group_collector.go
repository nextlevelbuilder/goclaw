package store

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
)

// GroupCollector wraps GroupStore with an in-memory cache.
// Only writes to DB when a group is new or its name has changed.
type GroupCollector struct {
	store GroupStore
	names cache.Cache[string] // channelType:grp:groupID → cached group name
}

// NewGroupCollector creates a new collector backed by the given store and cache.
func NewGroupCollector(s GroupStore, c cache.Cache[string]) *GroupCollector {
	return &GroupCollector{store: s, names: c}
}

// EnsureGroup upserts a group entry only if the name changed or is new.
func (c *GroupCollector) EnsureGroup(ctx context.Context, channelType, channelInstance, groupID, groupName string, memberCount int) {
	key := channelType + ":grp:" + groupID
	if cached, ok := c.names.Get(ctx, key); ok && cached == groupName {
		return // same name, skip DB write
	}
	if err := c.store.UpsertGroup(ctx, channelType, channelInstance, groupID, groupName, memberCount); err != nil {
		slog.Warn("group_collector.upsert_failed", "error", err, "channel", channelType, "group", groupID)
		return
	}
	c.names.Set(ctx, key, groupName, 0) // no TTL — evict only on name change
}
