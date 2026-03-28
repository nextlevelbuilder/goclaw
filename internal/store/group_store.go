package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ChannelGroup represents a known group/chat discovered through channel interactions.
type ChannelGroup struct {
	ID              uuid.UUID `json:"id"`
	ChannelType     string    `json:"channel_type"`
	ChannelInstance *string   `json:"channel_instance,omitempty"`
	GroupID         string    `json:"group_id"`
	GroupName       *string   `json:"group_name,omitempty"`
	AvatarURL       *string   `json:"avatar_url,omitempty"`
	MemberCount     int       `json:"member_count"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
}

// GroupStore manages the channel_groups directory.
type GroupStore interface {
	// UpsertGroup creates or updates a known group.
	UpsertGroup(ctx context.Context, channelType, channelInstance, groupID, groupName string, memberCount int) error

	// ListGroups returns all known groups for a channel type.
	ListGroups(ctx context.Context, channelType string) ([]ChannelGroup, error)

	// GetGroupsByIDs returns groups by their platform-specific IDs.
	GetGroupsByIDs(ctx context.Context, channelType string, groupIDs []string) (map[string]ChannelGroup, error)
}
