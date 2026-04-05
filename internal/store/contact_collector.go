package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
)

const contactSeenTTL = 30 * time.Minute

// ContactCollector wraps ContactStore with an in-memory "seen" cache
// to avoid redundant UPSERT queries on every message.
type ContactCollector struct {
	store ContactStore
	seen  cache.Cache[bool]
}

// NewContactCollector creates a new collector backed by the given store and cache.
func NewContactCollector(s ContactStore, c cache.Cache[bool]) *ContactCollector {
	return &ContactCollector{store: s, seen: c}
}

// EnsureContact creates or refreshes a contact entry, skipping DB if recently seen.
// contactType: "user" (individual sender), "group" (group chat entity), or "topic" (forum topic).
// Pass empty threadID/threadType for base contacts (DM, group root).
func (c *ContactCollector) EnsureContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string) {
	key := channelType + ":" + senderID + ":" + threadID
	if _, ok := c.seen.Get(ctx, key); ok {
		return
	}
	if contactType == "" {
		contactType = "user"
	}
	if err := c.store.UpsertContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType); err != nil {
		slog.Warn("contact_collector.upsert_failed", "error", err, "channel", channelType, "sender", senderID)
		return
	}
	c.seen.Set(ctx, key, true, contactSeenTTL)
}

// ResolveTenantUserID delegates to the underlying ContactStore.
func (c *ContactCollector) ResolveTenantUserID(ctx context.Context, channelType, senderID string) (string, error) {
	return c.store.ResolveTenantUserID(ctx, channelType, senderID)
}
