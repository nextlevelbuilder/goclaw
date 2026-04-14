package store

import (
	"context"

	"github.com/google/uuid"
)

// SkillInfo describes a discovered skill.
type SkillInfo struct {
	ID          string   `json:"id,omitempty" db:"id"` // DB UUID
	Name        string   `json:"name" db:"name"`
	Slug        string   `json:"slug" db:"slug"`
	Path        string   `json:"path" db:"path"`
	BaseDir     string   `json:"baseDir" db:"-"`
	Source      string   `json:"source" db:"-"`
	Description string   `json:"description" db:"description"`
	Visibility  string   `json:"visibility,omitempty" db:"visibility"`
	Tags        []string `json:"tags,omitempty" db:"tags"`
	Version     int      `json:"version,omitempty" db:"version"`
	IsSystem    bool     `json:"is_system,omitempty" db:"is_system"`
	Status      string   `json:"status,omitempty" db:"status"`
	Enabled     bool     `json:"enabled" db:"enabled"`
	Author      string   `json:"author,omitempty" db:"author"`
	MissingDeps []string `json:"missing_deps,omitempty" db:"missing_deps"`
}

// SkillSearchResult is a scored skill returned from embedding search.
type SkillSearchResult struct {
	Name        string  `json:"name" db:"name"`
	Slug        string  `json:"slug" db:"slug"`
	Description string  `json:"description" db:"description"`
	Path        string  `json:"path" db:"path"`
	Score       float64 `json:"score" db:"score"`
}

// SkillStore manages skill discovery and loading.
// Backed by Postgres (PGSkillStore) or filesystem (FileSkillStore).
type SkillStore interface {
	ListSkills(ctx context.Context) []SkillInfo
	LoadSkill(ctx context.Context, name string) (string, bool)
	LoadForContext(ctx context.Context, allowList []string) string
	BuildSummary(ctx context.Context, allowList []string) string
	GetSkill(ctx context.Context, name string) (*SkillInfo, bool)
	FilterSkills(ctx context.Context, allowList []string) []SkillInfo
	Version() int64
	BumpVersion()
	Dirs() []string
}

// SkillAccessStore is an optional interface for stores that support
// per-agent skill access filtering.
type SkillAccessStore interface {
	ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]SkillInfo, error)
}

// EmbeddingSkillSearcher is an optional interface for stores that support
// vector-based skill search. PGSkillStore implements this; FileSkillStore does not.
type EmbeddingSkillSearcher interface {
	SearchByEmbedding(ctx context.Context, embedding []float32, limit int) ([]SkillSearchResult, error)
	SetEmbeddingProvider(provider EmbeddingProvider)
	BackfillSkillEmbeddings(ctx context.Context) (int, error)
}

// SkillCreateParams holds parameters for creating a managed skill.
// Shared by PGSkillStore and SQLiteSkillStore.
type SkillCreateParams struct {
	Name        string
	Slug        string
	Description *string
	OwnerID     string
	Visibility  string
	Status      string // "active", "archived" (missing deps), or "deleted" (user-deleted)
	MissingDeps []string
	Version     int
	FilePath    string
	FileSize    int64
	FileHash    *string
	Frontmatter map[string]string
}

// SkillWithGrantStatus is a skill with its grant status for a specific agent.
type SkillWithGrantStatus struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Slug        string    `json:"slug" db:"slug"`
	Description string    `json:"description" db:"description"`
	Visibility  string    `json:"visibility" db:"visibility"`
	Version     int       `json:"version" db:"version"`
	Granted     bool      `json:"granted" db:"granted"`
	PinnedVer   *int      `json:"pinned_version,omitempty" db:"pinned_version"`
	IsSystem    bool      `json:"is_system" db:"is_system"`
}

// SkillManageStore extends SkillStore with CRUD, ownership, and grant operations
// needed by HTTP upload handlers and agent tools (skill_manage, publish_skill).
// Implemented by both PGSkillStore and SQLiteSkillStore.
type SkillManageStore interface {
	SkillStore
	// CRUD
	CreateSkillManaged(ctx context.Context, p SkillCreateParams) (uuid.UUID, error)
	UpdateSkill(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteSkill(ctx context.Context, id uuid.UUID) error
	ToggleSkill(ctx context.Context, id uuid.UUID, enabled bool) error
	// Queries
	GetSkillByID(ctx context.Context, id uuid.UUID) (SkillInfo, bool)
	GetSkillOwnerID(ctx context.Context, id uuid.UUID) (string, bool)
	GetSkillOwnerIDBySlug(ctx context.Context, slug string) (string, bool)
	GetNextVersion(ctx context.Context, slug string) int
	GetNextVersionLocked(ctx context.Context, slug string) (int, func() error, error)
	// GetSkillHashBySlug returns the content hash and version of the latest non-deleted skill
	// version for the given slug (tenant-scoped). Returns ok=false if no skill exists.
	GetSkillHashBySlug(ctx context.Context, slug string) (hash string, version int, ok bool)
	IsSystemSkill(slug string) bool
	// System skill management
	ListAllSkills(ctx context.Context) []SkillInfo
	ListAllSystemSkills(ctx context.Context) []SkillInfo
	ListSystemSkillDirs(ctx context.Context) map[string]string
	StoreMissingDeps(ctx context.Context, id uuid.UUID, missing []string) error
	// Grants
	GrantToAgent(ctx context.Context, skillID, agentID uuid.UUID, version int, grantedBy string) error
	RevokeFromAgent(ctx context.Context, skillID, agentID uuid.UUID) error
	GrantToUser(ctx context.Context, skillID uuid.UUID, userID, grantedBy string) error
	RevokeFromUser(ctx context.Context, skillID uuid.UUID, userID string) error
	ListWithGrantStatus(ctx context.Context, agentID uuid.UUID) ([]SkillWithGrantStatus, error)
	// Files
	GetSkillFilePath(ctx context.Context, id uuid.UUID) (filePath string, slug string, version int, isSystem bool, ok bool)
}
