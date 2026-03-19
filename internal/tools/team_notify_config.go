package tools

import "encoding/json"

// TeamNotifyConfig controls which team task events are forwarded to chat channels.
type TeamNotifyConfig struct {
	Dispatched bool   `json:"dispatched"` // task dispatched to member
	Progress   bool   `json:"progress"`   // member updates progress
	Failed     bool   `json:"failed"`     // task failed
	Completed  bool   `json:"completed"`  // task completed
	SlowTool   bool   `json:"slow_tool"`  // system alert when tool call exceeds adaptive threshold (always direct, never through leader)
	Mode       string `json:"mode"`       // "direct" (outbound) or "leader" (through leader agent)
}

// DefaultTeamNotifyConfig returns the default notification config.
func DefaultTeamNotifyConfig() TeamNotifyConfig {
	return TeamNotifyConfig{
		Dispatched: true,
		Progress:   true,
		Failed:     true,
		Completed:  true,
		SlowTool:   true,
		Mode:       "direct",
	}
}

// ParseTeamNotifyConfig extracts notification config from team settings JSON.
// Returns defaults for missing/invalid settings.
func ParseTeamNotifyConfig(settings json.RawMessage) TeamNotifyConfig {
	cfg := DefaultTeamNotifyConfig()
	if len(settings) == 0 {
		return cfg
	}
	var s struct {
		Notifications *struct {
			Dispatched *bool  `json:"dispatched"`
			Progress   *bool  `json:"progress"`
			Failed     *bool  `json:"failed"`
			Completed  *bool  `json:"completed"`
			SlowTool   *bool  `json:"slow_tool"`
			Mode       string `json:"mode"`
		} `json:"notifications"`
	}
	if json.Unmarshal(settings, &s) != nil || s.Notifications == nil {
		return cfg
	}
	n := s.Notifications
	if n.Dispatched != nil {
		cfg.Dispatched = *n.Dispatched
	}
	if n.Progress != nil {
		cfg.Progress = *n.Progress
	}
	if n.Failed != nil {
		cfg.Failed = *n.Failed
	}
	if n.Completed != nil {
		cfg.Completed = *n.Completed
	}
	if n.SlowTool != nil {
		cfg.SlowTool = *n.SlowTool
	}
	if n.Mode == "leader" {
		cfg.Mode = "leader"
	}
	return cfg
}
