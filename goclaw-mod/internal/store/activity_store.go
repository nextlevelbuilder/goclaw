package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ActivityLog represents a single audit log entry.
type ActivityLog struct {
	ID         uuid.UUID       `json:"id" db:"id"`
	ActorType  string          `json:"actor_type" db:"actor_type"`
	ActorID    string          `json:"actor_id" db:"actor_id"`
	Action     string          `json:"action" db:"action"`
	EntityType string          `json:"entity_type,omitempty" db:"entity_type"`
	EntityID   string          `json:"entity_id,omitempty" db:"entity_id"`
	Details    json.RawMessage `json:"details,omitempty" db:"details"`
	IPAddress  string          `json:"ip_address,omitempty" db:"ip_address"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// ActivityListOpts configures activity log listing.
type ActivityListOpts struct {
	ActorType  string
	ActorID    string
	Action     string
	EntityType string
	EntityID   string
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

// ActivityAggregateOpts configures audit log aggregation.
type ActivityAggregateOpts struct {
	ActivityListOpts
	GroupBy string
}

// ActivityAggregateBucket is a grouped audit log count.
type ActivityAggregateBucket struct {
	Key      string    `json:"key"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

// ActivityStore manages activity audit logs.
type ActivityStore interface {
	Log(ctx context.Context, entry *ActivityLog) error
	List(ctx context.Context, opts ActivityListOpts) ([]ActivityLog, error)
	Count(ctx context.Context, opts ActivityListOpts) (int, error)
	Aggregate(ctx context.Context, opts ActivityAggregateOpts) ([]ActivityAggregateBucket, int, error)
}
