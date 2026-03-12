package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ActivityLog represents a single audit log entry.
type ActivityLog struct {
	ID         uuid.UUID       `json:"id"`
	ActorType  string          `json:"actor_type"`
	ActorID    string          `json:"actor_id"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type,omitempty"`
	EntityID   string          `json:"entity_id,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
	IPAddress  string          `json:"ip_address,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ActivityListOpts configures activity log listing.
type ActivityListOpts struct {
	ActorType  string
	ActorID    string
	Action     string
	EntityType string
	EntityID   string
	Limit      int
	Offset     int
}

// ActivityStore manages activity audit logs.
type ActivityStore interface {
	Log(ctx context.Context, entry *ActivityLog) error
	List(ctx context.Context, opts ActivityListOpts) ([]ActivityLog, error)
	Count(ctx context.Context, opts ActivityListOpts) (int, error)
}
