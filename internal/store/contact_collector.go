package store

import (
	"context"
	"errors"
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
	// Cache key must include every dimension the underlying DB unique constraint
	// uses, otherwise dedup skips legitimate upserts:
	//   - tenantID: fixes cross-tenant leak (same sender in tenant A vs B)
	//   - channelInstance: fixes collision when two bots in the same tenant share
	//     overlapping sender ID spaces (e.g. two Telegram bot tokens with users
	//     who happen to have the same Telegram user_id)
	//   - threadID: different threads/topics track separate contacts
	// Zero UUID (Desktop / single-tenant) keeps legacy dedup semantics intact.
	tid := TenantIDFromContext(ctx)
	key := tid.String() + ":" + channelType + ":" + channelInstance + ":" + senderID + ":" + threadID
	if _, ok := c.seen.Get(ctx, key); ok {
		return
	}
	if contactType == "" {
		contactType = "user"
	}
	if err := c.store.UpsertContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType); err != nil {
		slog.Warn("contact_collector.upsert_failed",
			"error", err,
			"tenant_id", tid,
			"channel", channelType,
			"instance", channelInstance,
			"sender", senderID,
		)
		return
	}
	c.seen.Set(ctx, key, true, contactSeenTTL)
}

// RefreshContact upserts a contact without the short-lived hot-path dedup.
// Background metadata syncs use this so fresher platform names are persisted
// even when the message ingestion path recently saw the same ID.
func (c *ContactCollector) RefreshContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string) {
	if contactType == "" {
		contactType = "user"
	}
	if err := c.store.UpsertContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType); err != nil {
		slog.Warn("contact_collector.refresh_failed",
			"error", err,
			"tenant_id", TenantIDFromContext(ctx),
			"channel", channelType,
			"instance", channelInstance,
			"sender", senderID,
		)
	}
}

// EnsureContactWithMetadata is the metadata-aware counterpart of
// EnsureContact. It preserves the same hot-path dedup behavior and gracefully
// falls back for stores that only implement ContactStore.
func (c *ContactCollector) EnsureContactWithMetadata(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string, metadata map[string]string) {
	tid := TenantIDFromContext(ctx)
	key := tid.String() + ":" + channelType + ":" + channelInstance + ":" + senderID + ":" + threadID
	if _, ok := c.seen.Get(ctx, key); ok {
		return
	}
	if contactType == "" {
		contactType = "user"
	}
	metadataStore, ok := c.store.(ContactMetadataStore)
	if !ok {
		c.EnsureContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType)
		return
	}
	if err := metadataStore.UpsertContactWithMetadata(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType, metadata); err != nil {
		slog.Warn("contact_collector.upsert_metadata_failed",
			"error", err,
			"tenant_id", tid,
			"channel", channelType,
			"instance", channelInstance,
			"sender", senderID,
		)
		return
	}
	c.seen.Set(ctx, key, true, contactSeenTTL)
}

// RefreshContactWithMetadata persists presentation metadata without hot-path
// dedup, matching RefreshContact's refresh semantics.
func (c *ContactCollector) RefreshContactWithMetadata(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string, metadata map[string]string) {
	if contactType == "" {
		contactType = "user"
	}
	metadataStore, ok := c.store.(ContactMetadataStore)
	if !ok {
		c.RefreshContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType)
		return
	}
	if err := metadataStore.UpsertContactWithMetadata(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType, metadata); err != nil {
		slog.Warn("contact_collector.refresh_metadata_failed",
			"error", err,
			"tenant_id", TenantIDFromContext(ctx),
			"channel", channelType,
			"instance", channelInstance,
			"sender", senderID,
		)
	}
}

// ResolveTenantUserID delegates to the underlying ContactStore.
func (c *ContactCollector) ResolveTenantUserID(ctx context.Context, channelType, senderID string) (string, error) {
	return c.store.ResolveTenantUserID(ctx, channelType, senderID)
}

// ListContacts delegates contact discovery queries to the backing store.
// Background channel syncs use this to backfill metadata for previously seen
// contacts that are no longer present in a platform's live listing.
func (c *ContactCollector) ListContacts(ctx context.Context, opts ContactListOpts) ([]ChannelContact, error) {
	if c == nil || c.store == nil {
		return nil, errors.New("contact collector unavailable")
	}
	return c.store.ListContacts(ctx, opts)
}
