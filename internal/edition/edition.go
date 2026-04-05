// Package edition defines feature tiers for GoClaw.
// Set once at startup via SetCurrent(), read everywhere via Current().
// Adding a new edition = add one preset struct. No code changes elsewhere.
package edition

import "sync/atomic"

// Edition defines the feature limits for a GoClaw instance.
type Edition struct {
	Name                  string         `json:"name"`                    // "standard" or "lite"
	MaxAgents             int            `json:"max_agents"`              // 0 = unlimited
	MaxTeams              int            `json:"max_teams"`               // 0 = unlimited
	MaxTeamMembers        int            `json:"max_team_members"`        // 0 = unlimited
	MaxChannels           map[string]int `json:"max_channels"`            // per channel type, nil = unlimited
	MaxSubagentConcurrent int            `json:"max_subagent_concurrent"` // 0 = unlimited
	MaxSubagentDepth      int            `json:"max_subagent_depth"`      // 0 = use config default
	KGEnabled             bool           `json:"kg_enabled"`
	RBACEnabled           bool           `json:"rbac_enabled"`
	TeamFullMode          bool           `json:"team_full_mode"`          // false = lite task actions only
	VectorSearch          bool           `json:"vector_search"`           // false = FTS5 only
}

// --- Presets ---

// Standard is the default edition: all features enabled, no limits.
var Standard = Edition{
	Name:         "standard",
	KGEnabled:    true,
	RBACEnabled:  true,
	TeamFullMode: true,
	VectorSearch: true,
}

// Lite is the desktop/self-hosted edition with sensible limits.
var Lite = Edition{
	Name:                  "lite",
	MaxAgents:             5,
	MaxTeams:              1,
	MaxTeamMembers:        5,
	MaxChannels:           map[string]int{"telegram": 1, "discord": 1},
	MaxSubagentConcurrent: 2,
	MaxSubagentDepth:      1,
	KGEnabled:             false,
	RBACEnabled:           false,
	TeamFullMode:          false,
	VectorSearch:          false,
}

// --- Global state ---

// current holds the active edition. Atomic pointer for safe concurrent reads.
var current atomic.Pointer[Edition]

func init() {
	std := Standard
	current.Store(&std)
}

// Current returns the active edition. Safe for concurrent use.
func Current() Edition {
	return *current.Load()
}

// SetCurrent sets the active edition. Call once at startup.
func SetCurrent(e Edition) {
	current.Store(&e)
}

// --- Helpers ---

// IsLimited returns true if the edition enforces resource limits.
func (e Edition) IsLimited() bool {
	return e.MaxAgents > 0 || e.MaxTeams > 0
}

// ChannelLimit returns the max instances for a channel type.
// Returns 0 (unlimited) if the channel type is not in MaxChannels.
func (e Edition) ChannelLimit(channelType string) int {
	if e.MaxChannels == nil {
		return 0
	}
	return e.MaxChannels[channelType]
}
