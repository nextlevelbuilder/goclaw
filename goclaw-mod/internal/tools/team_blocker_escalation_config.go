package tools

import "encoding/json"

// BlockerEscalationConfig controls whether blocker comments trigger
// auto-fail + leader escalation.
type BlockerEscalationConfig struct {
	Enabled bool `json:"enabled"` // default: true
}

// DefaultBlockerEscalationConfig returns the default (enabled) config.
func DefaultBlockerEscalationConfig() BlockerEscalationConfig {
	return BlockerEscalationConfig{Enabled: true}
}

// ParseBlockerEscalationConfig extracts blocker escalation config from team settings.
func ParseBlockerEscalationConfig(settings json.RawMessage) BlockerEscalationConfig {
	cfg := DefaultBlockerEscalationConfig()
	if len(settings) == 0 {
		return cfg
	}
	var s struct {
		BlockerEscalation *struct {
			Enabled *bool `json:"enabled"`
		} `json:"blocker_escalation"`
	}
	if json.Unmarshal(settings, &s) != nil || s.BlockerEscalation == nil {
		return cfg
	}
	if s.BlockerEscalation.Enabled != nil {
		cfg.Enabled = *s.BlockerEscalation.Enabled
	}
	return cfg
}
