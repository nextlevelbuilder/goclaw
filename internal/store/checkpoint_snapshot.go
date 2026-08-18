package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CheckpointSnapshotStatus enumerates the run lifecycle statuses captured in a
// checkpoint-snapshot row. Values mirror the agent_runs status vocabulary so a
// snapshot row tells an operator what the run was doing when the snapshot was
// taken ("paused" | "running" | "compacting").
const (
	CheckpointSnapshotPaused     = "paused"
	CheckpointSnapshotRunning    = "running"
	CheckpointSnapshotCompacting = "compacting"
)

// ValidCheckpointSnapshotStatus reports whether s is a known snapshot status.
func ValidCheckpointSnapshotStatus(s string) bool {
	switch s {
	case CheckpointSnapshotPaused, CheckpointSnapshotRunning, CheckpointSnapshotCompacting:
		return true
	}
	return false
}

// CheckpointSnapshot is one append-only, versioned pipeline checkpoint row for a
// run. UpdateRunCheckpoint overwrites agent_runs.checkpoint with the latest
// snapshot; this table keeps the full history so a paused run can be replayed
// ("time travel") from ANY earlier snapshot seq. Snapshot carries the
// MarshalCheckpoint output, treated as opaque JSON by the store layer.
type CheckpointSnapshot struct {
	ID        uuid.UUID       `json:"id" db:"id"`
	TenantID  uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	RunID     string          `json:"run_id" db:"run_id"`
	Seq       int             `json:"seq" db:"seq"`
	Status    string          `json:"status,omitempty" db:"status"`
	Snapshot  json.RawMessage `json:"snapshot,omitempty" db:"snapshot"`
	Iteration int             `json:"iteration" db:"iteration"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

// CheckpointSnapshotListOpts scopes a checkpoint snapshot list read. Reads are
// tenant-scoped via context; RunID narrows the result set to one run's history.
type CheckpointSnapshotListOpts struct {
	RunID  string
	Limit  int
	Offset int
}

// CheckpointSnapshotStore persists append-only checkpoint-snapshot history for
// durable agent runs. All reads and writes are scoped to the context tenant and
// fail closed when a tenant is required but absent.
type CheckpointSnapshotStore interface {
	// AppendCheckpointSnapshot inserts a new snapshot row, assigning an ID,
	// CreatedAt, and the tenant when left empty. Status must be a known snapshot
	// status; Snapshot is stored as-is (JSONB in PostgreSQL, TEXT in SQLite).
	AppendCheckpointSnapshot(ctx context.Context, snap *CheckpointSnapshot) error
	// ListCheckpointSnapshots returns snapshot rows filtered by opts, scoped to
	// the context tenant. Newest first.
	ListCheckpointSnapshots(ctx context.Context, opts CheckpointSnapshotListOpts) ([]CheckpointSnapshot, error)
	// GetCheckpointSnapshot returns one snapshot by (run_id, seq), scoped to the
	// context tenant.
	GetCheckpointSnapshot(ctx context.Context, runID string, seq int) (*CheckpointSnapshot, error)
}