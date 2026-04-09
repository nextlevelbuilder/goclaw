package http

import (
	"time"
)

const maxExportSize = 500 << 20 // 500MB

// exportToken holds a short-lived reference to a completed export temp file.
type exportToken struct {
	agentID   string
	userID    string // creator — verified on download
	filePath  string
	fileName  string
	expiresAt time.Time
}

// Export token lifecycle managed by ExportTokenStore (see export_token_store.go).

// ExportManifest describes the archive contents.
type ExportManifest struct {
	Version    int            `json:"version"`
	Format     string         `json:"format"`
	ExportedAt string         `json:"exported_at"`
	ExportedBy string         `json:"exported_by"`
	AgentKey   string         `json:"agent_key"`
	AgentID    string         `json:"agent_id"`
	Sections   map[string]any `json:"sections"`
}

// KGEntityExport is a portable KG entity (no internal UUID).
type KGEntityExport struct {
	ExternalID  string            `json:"external_id"`
	UserID      string            `json:"user_id,omitempty"`
	Name        string            `json:"name"`
	EntityType  string            `json:"entity_type"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Confidence  float64           `json:"confidence"`
	ValidFrom   *time.Time        `json:"valid_from,omitempty"`
	ValidUntil  *time.Time        `json:"valid_until,omitempty"`
}

// KGRelationExport is a portable KG relation using external IDs.
type KGRelationExport struct {
	SourceExternalID string            `json:"source_external_id"`
	TargetExternalID string            `json:"target_external_id"`
	UserID           string            `json:"user_id,omitempty"`
	RelationType     string            `json:"relation_type"`
	Confidence       float64           `json:"confidence"`
	Properties       map[string]string `json:"properties,omitempty"`
	ValidFrom        *time.Time        `json:"valid_from,omitempty"`
	ValidUntil       *time.Time        `json:"valid_until,omitempty"`
}

// MemoryExport is a portable memory document.
type MemoryExport struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	UserID  string `json:"user_id,omitempty"`
}

// allExportSections is the complete set of exportable section keys.
var allExportSections = map[string]bool{
	"config":          true,
	"context_files":   true,
	"memory":          true,
	"knowledge_graph": true,
	"cron":            true,
	"user_profiles":   true,
	"user_overrides":  true,
	"workspace":       true,
	"team":            true,
	"episodic":        true,
	"evolution":       true,
	"vault":           true,
}
