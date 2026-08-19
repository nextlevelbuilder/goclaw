package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrMissionNotFound is returned by GetMission when no mission matches the
	// request. It is also wrapped by the mission resume closure when the mission
	// record or its owning agent run cannot be resolved.
	ErrMissionNotFound = errors.New("mission not found")
	// ErrMissionNotResumable is returned by the mission resume closure when the
	// mission is in a terminal state (completed/failed/cancelled) that cannot be
	// re-driven.
	ErrMissionNotResumable = errors.New("mission is not resumable")
)

// Mission status vocabulary. A mission is a durable, named objective with
// goals, milestones, and acceptance criteria. Status transitions:
//
//	active → paused ⇄ active
//	active → completed | failed | cancelled
//	paused → completed | failed | cancelled
//
// A paused mission is resumable through the mission cron branch (a cron job
// whose payload kind is "mission" carries the mission ID in Message).
const (
	MissionStatusActive    = "active"
	MissionStatusPaused    = "paused"
	MissionStatusCompleted = "completed"
	MissionStatusFailed    = "failed"
	MissionStatusCancelled = "cancelled"
)

// ValidMissionStatus reports whether s is a known mission status.
func ValidMissionStatus(s string) bool {
	switch s {
	case MissionStatusActive, MissionStatusPaused, MissionStatusCompleted,
		MissionStatusFailed, MissionStatusCancelled:
		return true
	}
	return false
}

// Mission is a durable, named objective owned by a tenant. Goals, Milestones,
// and Acceptance are ordered string lists; Metadata carries opaque JSON.
// CheckpointSeq links the mission to the latest durable run checkpoint
// snapshot seq so a resumed mission can pick up from where it left off.
type Mission struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	TenantID      uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	Name          string          `json:"name" db:"name"`
	Goals         []string        `json:"goals" db:"goals"`
	Milestones    []string        `json:"milestones" db:"milestones"`
	Acceptance    []string        `json:"acceptance" db:"acceptance"`
	Status        string          `json:"status" db:"status"`
	AgentID       *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	SessionKey    string          `json:"session_key" db:"session_key"`
	CheckpointSeq int             `json:"checkpoint_seq" db:"checkpoint_seq"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
	Metadata      json.RawMessage `json:"metadata,omitempty" db:"metadata"`
}

// MissionListOpts scopes a mission list read. Reads are tenant-scoped via
// context; Status narrows the result set to one mission lifecycle state.
type MissionListOpts struct {
	Status string
	Limit  int
	Offset int
}

// MissionStore persists durable missions. All reads and writes are scoped to
// the context tenant and fail closed when a tenant is required but absent.
type MissionStore interface {
	// CreateMission inserts a new mission, assigning an ID, CreatedAt,
	// UpdatedAt, and the tenant when left empty. Status must be a known mission
	// status (defaults to active).
	CreateMission(ctx context.Context, m *Mission) error
	// GetMission returns one mission by ID, scoped to the context tenant.
	GetMission(ctx context.Context, id uuid.UUID) (*Mission, error)
	// ListMissions returns missions filtered by opts, scoped to the context
	// tenant. Newest first.
	ListMissions(ctx context.Context, opts MissionListOpts) ([]Mission, error)
	// UpdateMissionStatus transitions a mission to a known status, scoped to the
	// context tenant. Bumps updated_at.
	UpdateMissionStatus(ctx context.Context, id uuid.UUID, status string) error
	// UpdateMissionProgress links the mission to the latest durable checkpoint
	// snapshot seq, scoped to the context tenant. Bumps updated_at.
	UpdateMissionProgress(ctx context.Context, id uuid.UUID, checkpointSeq int) error
	// DeleteMission removes a mission, scoped to the context tenant.
	DeleteMission(ctx context.Context, id uuid.UUID) error
}
