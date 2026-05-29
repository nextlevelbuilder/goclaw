package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WorkstationActivity is a single audit row for a workstation exec or deny event.
// Append-only; pruned nightly via Prune(before).
type WorkstationActivity struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenantId"`
	WorkstationID uuid.UUID  `json:"workstationId"`
	AgentID       string     `json:"agentId"`
	Action        string     `json:"action"`      // "exec" | "deny"
	CmdHash       string     `json:"cmdHash"`     // sha256 hex, first 16 chars shown
	CmdPreview    string     `json:"cmdPreview"`  // first 200 chars, secrets redacted
	ExitCode      *int       `json:"exitCode"`    // nil for deny rows
	DurationMS    *int64     `json:"durationMs"`  // nil for deny rows
	DenyReason    string     `json:"denyReason"`  // populated for action="deny"
	CreatedAt     time.Time  `json:"createdAt"`
}

// WorkstationActivityStore persists workstation exec and deny audit events.
type WorkstationActivityStore interface {
	// Insert adds a new activity row. Implementations may buffer writes for throughput.
	Insert(ctx context.Context, row *WorkstationActivity) error

	// List returns up to limit rows for a workstation, ordered by created_at DESC.
	// Pass cursor (last seen ID) to page. Returns next cursor (nil if no more rows).
	List(ctx context.Context, workstationID uuid.UUID, limit int, cursor *uuid.UUID) ([]WorkstationActivity, *uuid.UUID, error)

	// Prune deletes all rows created before the given time. Returns rows deleted.
	Prune(ctx context.Context, before time.Time) (int64, error)

	// Stop drains the write buffer and shuts down the background flusher goroutine.
	// Must be called on gateway shutdown to avoid losing buffered audit rows.
	Stop()
}
