// Package memory extends the v3 memory system with auto-injection and tiered retrieval.
//
// V3 design: Phase 3 — L0/L1/L2 context tiering + smart auto-inject.
package memory

import "context"

// AutoInjector checks relevance and produces L0 injection for system prompt.
// Called once per turn in ContextStage.
type AutoInjector interface {
	// Inject checks user message against memory index.
	// Returns formatted section for system prompt, or "" if nothing relevant.
	// Budget: max ~200 tokens of L0 summaries.
	Inject(ctx context.Context, params InjectParams) (*InjectResult, error)
}

// InjectParams configures a single auto-inject call.
type InjectParams struct {
	AgentID     string
	UserID      string
	TenantID    string
	UserMessage string
	MaxEntries  int     // default 5
	MaxTokens   int     // default 200
	Threshold   float64 // relevance threshold (default 0.3)
}

// InjectResult contains the injection output + observability data.
type InjectResult struct {
	Section    string  // formatted prompt section (empty = nothing relevant)
	MatchCount int     // total matches found
	Injected   int     // entries injected (after budget trim)
	TopScore   float64 // highest relevance score
}

// L0Summary is a single auto-inject entry for the system prompt.
type L0Summary struct {
	Topic   string // short topic label
	Summary string // ~1 sentence abstract
	ID      string // for memory_expand(id) deep retrieval
}

// MemoryConfig holds per-agent memory settings (stored in agents.settings JSONB).
type MemoryConfig struct {
	AutoInjectEnabled   bool    `json:"auto_inject_enabled"`    // default true
	AutoInjectThreshold float64 `json:"auto_inject_threshold"`  // default 0.3
	AutoInjectMaxTokens int     `json:"auto_inject_max_tokens"` // default 200
	EpisodicTTLDays     int     `json:"episodic_ttl_days"`      // default 90
	ConsolidationEnabled bool   `json:"consolidation_enabled"`  // default true
}

// DefaultMemoryConfig returns sensible defaults.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		AutoInjectEnabled:    true,
		AutoInjectThreshold:  0.3,
		AutoInjectMaxTokens:  200,
		EpisodicTTLDays:      90,
		ConsolidationEnabled: true,
	}
}
