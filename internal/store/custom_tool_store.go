package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// CustomToolDef represents a custom tool definition in the database.
type CustomToolDef struct {
	BaseModel
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Parameters     json.RawMessage `json:"parameters"`
	Command        string          `json:"command"`
	WorkingDir     string          `json:"working_dir,omitempty"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Env            []byte          `json:"-"` // encrypted JSONB — never serialized to API
	AgentID        *uuid.UUID      `json:"agent_id,omitempty"`
	Enabled        bool            `json:"enabled"`
	CreatedBy      string          `json:"created_by"`
}

// CustomToolListOpts configures custom tool listing with optional pagination and filtering.
type CustomToolListOpts struct {
	AgentID *uuid.UUID
	Search  string
	Limit   int
	Offset  int
}

// CustomToolStore manages custom tool definitions.
type CustomToolStore interface {
	Create(ctx context.Context, def *CustomToolDef) error
	Get(ctx context.Context, id uuid.UUID) (*CustomToolDef, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListGlobal(ctx context.Context) ([]CustomToolDef, error)
	ListByAgent(ctx context.Context, agentID uuid.UUID) ([]CustomToolDef, error)
	ListAll(ctx context.Context) ([]CustomToolDef, error)
	ListPaged(ctx context.Context, opts CustomToolListOpts) ([]CustomToolDef, error)
	CountTools(ctx context.Context, opts CustomToolListOpts) (int, error)
}
