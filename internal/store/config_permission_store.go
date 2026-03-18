package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ConfigPermission represents an allow/deny rule for agent configuration.
type ConfigPermission struct {
	ID         uuid.UUID       `json:"id"`
	AgentID    uuid.UUID       `json:"agentId"`
	Scope      string          `json:"scope"`      // "agent" | "group:telegram:-100456" | "group:*" | "*"
	ConfigType string          `json:"configType"` // "heartbeat" | "cron" | "context_files" | "*"
	UserID     string          `json:"userId"`
	Permission string          `json:"permission"` // "allow" | "deny"
	GrantedBy  *string         `json:"grantedBy,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// ConfigPermissionStore manages agent configuration permissions with wildcard scope matching.
type ConfigPermissionStore interface {
	// CheckPermission checks if a user has permission for a given config action.
	// Evaluates deny rules first, then allow rules, using Go-level wildcard matching.
	CheckPermission(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error)

	Grant(ctx context.Context, perm *ConfigPermission) error
	Revoke(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) error
	List(ctx context.Context, agentID uuid.UUID, configType string) ([]ConfigPermission, error)
}
