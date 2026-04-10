package store

import "encoding/json"

// V3Flags holds per-agent v3 feature flags stored in other_config JSONB.
// All flags default to false (v2 behavior) when missing or malformed.
type V3Flags struct {
	PipelineEnabled  bool `json:"v3_pipeline_enabled" db:"-"`  // Deprecated: always true. Kept for JSONB backward compat.
	MemoryEnabled    bool `json:"v3_memory_enabled" db:"-"`    // Deprecated: always true at runtime. Kept for JSONB backward compat.
	RetrievalEnabled bool `json:"v3_retrieval_enabled" db:"-"` // Deprecated: always true at runtime. Kept for JSONB backward compat.
	EvolutionMetrics bool `json:"self_evolution_metrics" db:"-"`
	EvolutionSuggest bool `json:"self_evolution_suggestions" db:"-"`
}

// v3FlagKeys lists all recognized v3 flag keys for validation.
var v3FlagKeys = map[string]bool{
	"v3_pipeline_enabled":        true,
	"v3_memory_enabled":          true,
	"v3_retrieval_enabled":       true,
	"self_evolution_metrics":     true,
	"self_evolution_suggestions": true,
}

// IsV3FlagKey reports whether key is a recognized v3 feature flag.
func IsV3FlagKey(key string) bool { return v3FlagKeys[key] }

// ParseV3Flags extracts v3 feature flags from other_config JSONB.
// Returns zero-value struct (all false) on missing/malformed data.
func (a *AgentData) ParseV3Flags() V3Flags {
	if len(a.OtherConfig) <= 2 {
		return V3Flags{}
	}
	var flags V3Flags
	if json.Unmarshal(a.OtherConfig, &flags) != nil {
		return V3Flags{}
	}
	return flags
}
