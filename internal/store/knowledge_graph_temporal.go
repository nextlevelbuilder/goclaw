package store

import (
	"context"
	"time"
)

// TemporalQueryOptions extends entity queries with time awareness.
type TemporalQueryOptions struct {
	AsOf           *time.Time // point-in-time query (nil = current only)
	IncludeExpired bool       // include superseded facts
}

// KGConfig holds per-agent dedup thresholds (stored in agents.other_config JSONB).
type KGConfig struct {
	DedupAutoThreshold float64 `json:"kg_dedup_auto_threshold"` // default 0.98
	DedupFlagThreshold float64 `json:"kg_dedup_flag_threshold"` // default 0.90
	ExtractionMinConf  float64 `json:"kg_extraction_min_conf"`  // default 0.75
	EnableTemporal     bool    `json:"kg_enable_temporal"`       // default true
}

// DefaultKGConfig returns sensible defaults matching current hardcoded values.
func DefaultKGConfig() KGConfig {
	return KGConfig{
		DedupAutoThreshold: 0.98,
		DedupFlagThreshold: 0.90,
		ExtractionMinConf:  0.75,
		EnableTemporal:     true,
	}
}

// KnowledgeGraphTemporalStore extends KnowledgeGraphStore with temporal methods.
// These will be added to the existing KnowledgeGraphStore interface in implementation.
type KnowledgeGraphTemporalStore interface {
	// ListEntitiesTemporal queries entities with temporal awareness.
	// When opts.AsOf is nil, returns current facts only (valid_until IS NULL).
	ListEntitiesTemporal(ctx context.Context, agentID, userID string,
		listOpts EntityListOptions, temporal TemporalQueryOptions) ([]Entity, error)

	// SupersedeEntity marks entity as no longer valid and inserts replacement.
	// Atomic: UPDATE old SET valid_until=NOW() + INSERT new in single tx.
	SupersedeEntity(ctx context.Context, old *Entity, replacement *Entity) error

	// GetDedupConfig returns per-agent dedup thresholds from agents.other_config.
	GetDedupConfig(ctx context.Context, agentID string) (*KGConfig, error)
}
