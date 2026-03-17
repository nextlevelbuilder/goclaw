package tools

import "encoding/json"

// TeamNotifyConfig controls which team task events are forwarded to chat channels.
type TeamNotifyConfig struct {
	Dispatched bool   `json:"dispatched"` // task assigned to member
	Progress   bool   `json:"progress"`   // member updates progress
	Failed     bool   `json:"failed"`     // task failed
	Mode       string `json:"mode"`       // "direct" (outbound) or "leader" (through leader agent)
}

// DefaultTeamNotifyConfig returns the default notification config.
func DefaultTeamNotifyConfig() TeamNotifyConfig {
	return TeamNotifyConfig{
		Dispatched: true,
		Progress:   true,
		Failed:     true,
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
		Notifications *TeamNotifyConfig `json:"notifications"`
	}
	if json.Unmarshal(settings, &s) != nil || s.Notifications == nil {
		return cfg
	}
	n := s.Notifications
	cfg.Dispatched = n.Dispatched
	cfg.Progress = n.Progress
	cfg.Failed = n.Failed
	if n.Mode == "leader" {
		cfg.Mode = "leader"
	}
	return cfg
}
