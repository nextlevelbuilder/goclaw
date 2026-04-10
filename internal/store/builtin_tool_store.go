package store

import (
	"context"
	"encoding/json"
	"time"
)

// BuiltinToolDef represents a built-in tool definition in the database.
// Built-in tools are seeded at startup and can be enabled/disabled or configured
// via the settings JSONB column.
type BuiltinToolDef struct {
	Name        string          `json:"name" db:"name"`
	DisplayName string          `json:"display_name" db:"display_name"`
	Description string          `json:"description" db:"description"`
	Category    string          `json:"category" db:"category"`
	Enabled     bool            `json:"enabled" db:"enabled"`
	Settings    json.RawMessage `json:"settings" db:"settings"`
	Requires    []string        `json:"requires,omitempty" db:"requires"`
	Metadata    json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// BuiltinToolStore manages built-in tool definitions.
// Built-in tools are seeded on startup; only enabled/settings are user-editable.
type BuiltinToolStore interface {
	List(ctx context.Context) ([]BuiltinToolDef, error)
	Get(ctx context.Context, name string) (*BuiltinToolDef, error)
	Update(ctx context.Context, name string, updates map[string]any) error
	Seed(ctx context.Context, tools []BuiltinToolDef) error
	ListEnabled(ctx context.Context) ([]BuiltinToolDef, error)
	GetSettings(ctx context.Context, name string) (json.RawMessage, error)
}
