package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ChannelContact represents a user discovered through channel interactions.
// Global (not per-agent): same person on the same platform = one row.
type ChannelContact struct {
	ID              uuid.UUID  `json:"id"`
	ChannelType     string     `json:"channel_type"`
	ChannelInstance *string    `json:"channel_instance,omitempty"`
	SenderID        string     `json:"sender_id"`
	UserID          *string    `json:"user_id,omitempty"`
	DisplayName     *string    `json:"display_name,omitempty"`
	Username        *string    `json:"username,omitempty"`
	AvatarURL       *string    `json:"avatar_url,omitempty"`
	PeerKind        *string    `json:"peer_kind,omitempty"`
	MergedID        *uuid.UUID `json:"merged_id,omitempty"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
}

// ContactListOpts holds pagination and filter options for listing contacts.
type ContactListOpts struct {
	Search      string // ILIKE on display_name, username, sender_id
	ChannelType string // filter by platform (telegram, discord, etc.)
	PeerKind    string // "direct" or "group"
	Limit       int
	Offset      int
}

// ContactStore manages channel contacts (auto-collected user info).
type ContactStore interface {
	// UpsertContact creates or updates a contact. On conflict (channel_type, sender_id),
	// updates display_name, username, user_id, channel_instance, and last_seen_at.
	UpsertContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind string) error

	// ListContacts searches contacts with pagination and filters.
	ListContacts(ctx context.Context, opts ContactListOpts) ([]ChannelContact, error)

	// CountContacts returns total matching contacts for the given filters.
	CountContacts(ctx context.Context, opts ContactListOpts) (int, error)

	// GetContactsBySenderIDs returns contacts matching the given sender IDs.
	// Returns a map of sender_id → ChannelContact (first match per sender_id).
	GetContactsBySenderIDs(ctx context.Context, senderIDs []string) (map[string]ChannelContact, error)

	// GetContactByID returns a single contact by primary key. Tenant-scoped via context.
	GetContactByID(ctx context.Context, id uuid.UUID) (*ChannelContact, error)

	// MergeContacts sets merged_id = tenantUserID on all given contact IDs,
	// linking them to a tenant_users identity. Tenant-scoped via context.
	MergeContacts(ctx context.Context, contactIDs []uuid.UUID, tenantUserID uuid.UUID) error

	// UnmergeContacts sets merged_id = NULL on the given contact IDs.
	// Tenant-scoped via context.
	UnmergeContacts(ctx context.Context, contactIDs []uuid.UUID) error

	// GetContactsByMergedID returns all contacts linked to a given merged_id.
	// Tenant-scoped via context.
	GetContactsByMergedID(ctx context.Context, mergedID uuid.UUID) ([]ChannelContact, error)
}
