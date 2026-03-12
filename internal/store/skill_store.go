package store

import (
	"context"

	"github.com/google/uuid"
)

// SkillInfo describes a discovered skill.
type SkillInfo struct {
	ID          string   `json:"id,omitempty"` // DB UUID
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Path        string   `json:"path"`
	BaseDir     string   `json:"baseDir"`
	Source      string   `json:"source"`
	Description string   `json:"description"`
	Visibility  string   `json:"visibility,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Version     int      `json:"version,omitempty"`
	IsSystem    bool     `json:"is_system,omitempty"`
	Status      string   `json:"status,omitempty"`
	Enabled     bool     `json:"enabled"`
	Author      string   `json:"author,omitempty"`
	MissingDeps []string `json:"missing_deps,omitempty"`
}

// SkillSearchResult is a scored skill returned from embedding search.
type SkillSearchResult struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	Path        string  `json:"path"`
	Score       float64 `json:"score"`
}

// SkillStore manages skill discovery and loading.
// Backed by Postgres (PGSkillStore) or filesystem (FileSkillStore).
type SkillStore interface {
	ListSkills() []SkillInfo
	LoadSkill(name string) (string, bool)
	LoadForContext(allowList []string) string
	BuildSummary(allowList []string) string
	GetSkill(name string) (*SkillInfo, bool)
	FilterSkills(allowList []string) []SkillInfo
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
