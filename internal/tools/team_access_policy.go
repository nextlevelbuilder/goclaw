package tools

import (
	"encoding/json"
	"fmt"
	"slices"
)

// teamAccessSettings defines access control rules stored in agent_teams.settings JSONB.
// Empty/nil lists mean "no restriction". Deny lists take precedence over allow lists.
type teamAccessSettings struct {
	Version               *int     `json:"version,omitempty"`
	AllowUserIDs          []string `json:"allow_user_ids"`
	DenyUserIDs           []string `json:"deny_user_ids"`
	AllowChannels         []string `json:"allow_channels"`
	DenyChannels          []string `json:"deny_channels"`
	Notifications        *TeamNotifyConfig `json:"notifications,omitempty"`
	FollowupIntervalMins *int              `json:"followup_interval_minutes,omitempty"`
	FollowupMaxReminders  *int     `json:"followup_max_reminders,omitempty"`
	EscalationMode        string   `json:"escalation_mode,omitempty"`
	EscalationActions     []string `json:"escalation_actions,omitempty"`
}

// checkTeamAccess validates whether a user/channel combination is authorized
// for team operations. Returns nil if access is allowed.
// System channels (ChannelDelegate, ChannelSystem) always pass.
// Empty settings = open access (no restrictions).
func checkTeamAccess(settings json.RawMessage, userID, channel string) error {
	if len(settings) == 0 || string(settings) == "{}" {
		return nil
	}
	var s teamAccessSettings
	if json.Unmarshal(settings, &s) != nil {
		return nil // malformed = fail open
	}

	// System/internal access always allowed
	if channel == ChannelDelegate || channel == ChannelSystem {
		return nil
	}

	// User check: deny > allow
	if userID != "" {
		if slices.Contains(s.DenyUserIDs, userID) {
			return fmt.Errorf("user not authorized for this team")
		}
		if len(s.AllowUserIDs) > 0 {
			found := slices.Contains(s.AllowUserIDs, userID)
			if !found {
				return fmt.Errorf("user not authorized for this team")
			}
		}
	}

	// Channel check: deny > allow
	if channel != "" {
		if slices.Contains(s.DenyChannels, channel) {
			return fmt.Errorf("channel %q not authorized for this team", channel)
		}
		if len(s.AllowChannels) > 0 {
			found := slices.Contains(s.AllowChannels, channel)
			if !found {
				return fmt.Errorf("channel %q not authorized for this team", channel)
			}
		}
	}

	return nil
}
