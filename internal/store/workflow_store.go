package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Workflow is a saved, triggerable graph: "when X, run Y".
//
// The graph is the authoring source of truth; it is COMPILED into the primitives
// that actually run things (cron entries today, channel subscriptions later),
// and those primitives stay authoritative at execution time. See migration
// 000083 for why the split exists.
type Workflow struct {
	ID          uuid.UUID `json:"id" db:"id"`
	TenantID    uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`

	// Enabled = ARMED. A disabled workflow is saved and editable but nothing it
	// describes will fire, which is the normal state while it is being built.
	Enabled bool `json:"enabled" db:"enabled"`

	// Graph is the authored {nodes, edges} blob. Opaque to the store — the shape
	// belongs to the canvas and the compiler, and will change as the revamp
	// proceeds; teaching the store about it would mean a migration per UI change.
	Graph json.RawMessage `json:"graph" db:"graph"`

	// Compiled records what the last successful compile CREATED, so the
	// reconciler can retract exactly that (by id) rather than re-deriving it from
	// a graph that may since have been edited. Retracting from a stale derivation
	// is how orphaned schedules happen.
	Compiled json.RawMessage `json:"compiled" db:"compiled"`

	// CompileError is the last failure, surfaced in the UI. nil = last compile
	// was clean, or none has run.
	CompileError *string `json:"compile_error,omitempty" db:"compile_error"`

	CreatedBy *string   `json:"created_by,omitempty" db:"created_by"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ErrWorkflowNameTaken is returned when a tenant already has a workflow with
// that name (compared case-insensitively). Surfaced as a distinct error because
// the UI must say "that name is taken" rather than "save failed".
var ErrWorkflowNameTaken = errors.New("a workflow with that name already exists")

// WorkflowStore persists authored workflows.
//
// Every method is TENANT-SCOPED by argument rather than by ambient context: a
// workflow names a tenant's own agents, so a cross-tenant read would leak the
// shape of someone else's automation, and a cross-tenant write would arm it.
// Passing the tenant explicitly makes that impossible to forget at a call site.
type WorkflowStore interface {
	// ListForTenant returns a tenant's workflows, most recently updated first.
	ListForTenant(ctx context.Context, tenantID uuid.UUID) ([]*Workflow, error)

	// Get returns one workflow. Returns nil (no error) when it does not exist or
	// belongs to another tenant — the caller cannot distinguish the two, so a
	// guessed id reveals nothing.
	Get(ctx context.Context, tenantID, id uuid.UUID) (*Workflow, error)

	// Create inserts a workflow. Returns ErrWorkflowNameTaken on a name clash.
	Create(ctx context.Context, w *Workflow) error

	// Update replaces the mutable fields of an existing workflow (name,
	// description, enabled, graph). Compile results are written by
	// SetCompileResult, not here: a user saving a graph must never be able to
	// silently overwrite what the reconciler recorded.
	Update(ctx context.Context, w *Workflow) error

	// SetCompileResult records the outcome of a compile. compiled is what was
	// created; compileErr nil means success.
	SetCompileResult(ctx context.Context, tenantID, id uuid.UUID, compiled json.RawMessage, compileErr *string) error

	// Delete removes a workflow. Callers must retract its compiled primitives
	// FIRST — the row carries the only record of what to retract.
	Delete(ctx context.Context, tenantID, id uuid.UUID) error

	// ListEnabled returns every armed workflow across all tenants, for the
	// reconciler to rebuild schedules on startup. Not client-facing.
	ListEnabled(ctx context.Context) ([]*Workflow, error)
}
