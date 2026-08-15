package tools

import "encoding/json"

// MemberRequestConfig controls whether team members can create "request" tasks.
type MemberRequestConfig struct {
	Enabled      bool `json:"enabled"`       // allow members to create task_type="request"
	AutoDispatch bool `json:"auto_dispatch"` // auto-dispatch requests to assignee (vs. pending for leader)
}

// DefaultMemberRequestConfig returns the default (disabled) config.
func DefaultMemberRequestConfig() MemberRequestConfig {
	return MemberRequestConfig{Enabled: false, AutoDispatch: false}
}

// ParseMemberRequestConfig extracts member request config from team settings JSON.
func ParseMemberRequestConfig(settings json.RawMessage) MemberRequestConfig {
	cfg := DefaultMemberRequestConfig()
	if len(settings) == 0 {
		return cfg
	}
	var s struct {
		MemberRequests *struct {
			Enabled      *bool `json:"enabled"`
			AutoDispatch *bool `json:"auto_dispatch"`
		} `json:"member_requests"`
	}
	if json.Unmarshal(settings, &s) != nil || s.MemberRequests == nil {
		return cfg
	}
	if s.MemberRequests.Enabled != nil {
		cfg.Enabled = *s.MemberRequests.Enabled
	}
	if s.MemberRequests.AutoDispatch != nil {
		cfg.AutoDispatch = *s.MemberRequests.AutoDispatch
	}
	return cfg
}
