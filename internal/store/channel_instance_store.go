package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// ChannelInstanceData represents a channel instance in the database.
type ChannelInstanceData struct {
	BaseModel
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	ChannelType string          `json:"channel_type"`
	AgentID     uuid.UUID       `json:"agent_id"`
	Credentials []byte          `json:"-"` // encrypted, never serialized to API
	Config      json.RawMessage `json:"config"`
	Enabled     bool            `json:"enabled"`
	CreatedBy   string          `json:"created_by"`
}

// IsDefaultChannelInstance returns true if the instance name matches a default/seeded channel.
// Default instances use either the bare channel type ("telegram") or "{channelType}/default".
func IsDefaultChannelInstance(name string) bool {
	if strings.HasSuffix(name, "/default") {
		return true
	}
	// Legacy Telegram default uses bare name "telegram"
	switch name {
	case "telegram", "discord", "feishu", "zalo_oa", "whatsapp":
		return true
	}
	return false
}

// ChannelInstanceListOpts configures channel instance listing with optional pagination and filtering.
type ChannelInstanceListOpts struct {
	Search string
	Limit  int
	Offset int
}

// ChannelInstanceStore manages channel instance definitions.
type ChannelInstanceStore interface {
	Create(ctx context.Context, inst *ChannelInstanceData) error
	Get(ctx context.Context, id uuid.UUID) (*ChannelInstanceData, error)
	GetByName(ctx context.Context, name string) (*ChannelInstanceData, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListEnabled(ctx context.Context) ([]ChannelInstanceData, error)
	ListAll(ctx context.Context) ([]ChannelInstanceData, error)
	ListPaged(ctx context.Context, opts ChannelInstanceListOpts) ([]ChannelInstanceData, error)
	CountInstances(ctx context.Context, opts ChannelInstanceListOpts) (int, error)
}
