package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ManagedToolInfo describes a managed tool.
type ManagedToolInfo struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	Description string            `json:"description"`
	Visibility  string            `json:"visibility,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Version     int               `json:"version,omitempty"`
	Status      string            `json:"status,omitempty"`
	Enabled     bool              `json:"enabled"`
	Runtime     *string           `json:"runtime,omitempty"`
	EntryPoint  *string           `json:"entry_point,omitempty"`
	FilePath    string            `json:"file_path"`
	IsSystem    bool              `json:"is_system,omitempty"`
	OwnerID     string            `json:"owner_id,omitempty"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
	FileSize    int64             `json:"file_size,omitempty"`
	FileHash    *string           `json:"file_hash,omitempty"`
	CreatedAt   *time.Time        `json:"created_at,omitempty"`
	UpdatedAt   *time.Time        `json:"updated_at,omitempty"`
}

// ManagedToolCreateParams holds parameters for creating a managed tool.
type ManagedToolCreateParams struct {
	Name        string
	Slug        string
	Description *string
	OwnerID     string
	Visibility  string
	Version     int
	FilePath    string
	FileSize    int64
	FileHash    *string
	Frontmatter map[string]string
	Runtime     *string
	EntryPoint  *string
}

// ManagedToolStore manages managed tool CRUD and discovery.
type ManagedToolStore interface {
	ListManagedTools() []ManagedToolInfo
	GetManagedTool(slug string) (*ManagedToolInfo, bool)
	GetManagedToolByID(id uuid.UUID) (ManagedToolInfo, bool)
	CreateManagedTool(ctx context.Context, p ManagedToolCreateParams) (uuid.UUID, error)
	UpdateManagedTool(id uuid.UUID, updates map[string]any) error
	DeleteManagedTool(id uuid.UUID) error
	ToggleManagedTool(id uuid.UUID, enabled bool) error
	BumpVersion()
	Version() int64
	Dirs() []string
}
