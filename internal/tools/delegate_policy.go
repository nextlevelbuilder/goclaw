package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func checkUserPermission(settings json.RawMessage, userID string) error {
	if len(settings) == 0 || string(settings) == "{}" {
		return nil
	}
	var s linkSettings
	if json.Unmarshal(settings, &s) != nil {
		return nil // malformed = fail open
	}
	if slices.Contains(s.UserDeny, userID) {
		return fmt.Errorf("you are not authorized to use this delegation link")
	}
	if len(s.UserAllow) > 0 {
		if slices.Contains(s.UserAllow, userID) {
			return nil
		}
		return fmt.Errorf("you are not authorized to use this delegation link")
	}
	return nil
}

// teamAccessSettings defines access control rules stored in agent_teams.settings JSONB.
// Empty/nil lists mean "no restriction". Deny lists take precedence over allow lists.
type teamAccessSettings struct {
	Version               *int     `json:"version,omitempty"`
	AllowUserIDs          []string `json:"allow_user_ids"`
	DenyUserIDs           []string `json:"deny_user_ids"`
	AllowChannels         []string `json:"allow_channels"`
	DenyChannels          []string `json:"deny_channels"`
	ProgressNotifications *bool    `json:"progress_notifications,omitempty"`
	FollowupIntervalMins  *int     `json:"followup_interval_minutes,omitempty"`
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

func parseMaxDelegationLoad(otherConfig json.RawMessage) int {
	if len(otherConfig) == 0 {
		return defaultMaxDelegationLoad
	}
	var cfg struct {
		MaxDelegationLoad int `json:"max_delegation_load"`
	}
	if json.Unmarshal(otherConfig, &cfg) != nil || cfg.MaxDelegationLoad <= 0 {
		return defaultMaxDelegationLoad
	}
	return cfg.MaxDelegationLoad
}

func parseQualityGates(otherConfig json.RawMessage) []hooks.HookConfig {
	if len(otherConfig) == 0 {
		return nil
	}
	var cfg struct {
		QualityGates []hooks.HookConfig `json:"quality_gates"`
	}
	if json.Unmarshal(otherConfig, &cfg) != nil {
		return nil
	}
	return cfg.QualityGates
}

func parseProgressNotifications(settings json.RawMessage, globalDefault bool) bool {
	if len(settings) == 0 {
		return globalDefault
	}
	var s teamAccessSettings
	if json.Unmarshal(settings, &s) != nil {
		return globalDefault
	}
	if s.ProgressNotifications != nil {
		return *s.ProgressNotifications
	}
	return globalDefault
}

// applyQualityGates evaluates quality gates on a delegation result.
// Returns the (possibly revised) result. If a blocking gate fails after all retries,
// returns the last result anyway with a logged warning (does not hard-fail the delegation).
// Only returns error on catastrophic failures (e.g. context cancelled).
func (dm *DelegateManager) applyQualityGates(
	ctx context.Context, task *DelegationTask, opts DelegateOpts,
	result *DelegateRunResult,
) (*DelegateRunResult, error) {
	if dm.hookEngine == nil || hooks.SkipHooksFromContext(ctx) {
		return result, nil
	}

	sourceAgent, err := dm.agentStore.GetByID(ctx, task.SourceAgentID)
	if err != nil || sourceAgent == nil {
		return result, nil
	}

	gates := parseQualityGates(sourceAgent.OtherConfig)
	if len(gates) == 0 {
		return result, nil
	}

	hctx := hooks.HookContext{
		Event:          "delegation.completed",
		SourceAgentKey: task.SourceAgentKey,
		TargetAgentKey: task.TargetAgentKey,
		UserID:         task.UserID,
		Content:        result.Content,
		Task:           opts.Task,
	}

	for _, gate := range gates {
		if gate.Event != "delegation.completed" {
			continue
		}

		currentResult := result
		retries := gate.MaxRetries

		for attempt := 0; attempt <= retries; attempt++ {
			hctx.Content = currentResult.Content

			hookResult, evalErr := dm.hookEngine.EvaluateSingleHook(ctx, gate, hctx)
			if evalErr != nil {
				slog.Warn("quality_gate: evaluator error, skipping",
					"type", gate.Type, "delegation", task.ID, "error", evalErr)
				break
			}

			if hookResult.Passed {
				result = currentResult
				break
			}

			// Gate failed
			if !gate.BlockOnFailure {
				slog.Warn("quality_gate: non-blocking gate failed",
					"type", gate.Type, "delegation", task.ID)
				break
			}

			if attempt >= retries {
				slog.Warn("quality_gate: max retries exceeded, accepting result",
					"type", gate.Type, "delegation", task.ID, "retries", retries)
				result = currentResult
				break
			}

			// Retry: re-run target agent with feedback
			slog.Info("quality_gate: retrying delegation",
				"type", gate.Type, "delegation", task.ID,
				"attempt", attempt+1, "max_retries", retries)

			// Emit quality gate retry event for WS visibility.
			if dm.msgBus != nil {
				dm.msgBus.Broadcast(bus.Event{
					Name: protocol.EventQualityGateRetry,
					Payload: protocol.QualityGateRetryPayload{
						DelegationID:   task.ID,
						TargetAgentKey: task.TargetAgentKey,
						UserID:         task.UserID,
						Channel:        task.OriginChannel,
						ChatID:         task.OriginChatID,
						TeamID: func() string {
							if task.TeamID != uuid.Nil {
								return task.TeamID.String()
							}
							return ""
						}(),
						TeamTaskID: func() string {
							if task.TeamTaskID != uuid.Nil {
								return task.TeamTaskID.String()
							}
							return ""
						}(),
						GateType:   string(gate.Type),
						Attempt:    attempt + 1,
						MaxRetries: retries,
						Feedback:   hookResult.Feedback,
					},
				})
			}

			feedbackMsg := fmt.Sprintf(
				"[Quality Gate Feedback — Retry %d/%d]\n"+
					"Your previous output did not pass quality review.\n\n"+
					"Feedback: %s\n\n"+
					"Original task: %s\n\n"+
					"Please revise your output addressing the feedback.",
				attempt+1, retries, hookResult.Feedback, opts.Task)

			rerunResult, rerunErr := dm.runAgent(ctx, opts.TargetAgentKey, dm.buildRunRequest(task, feedbackMsg))
			if rerunErr != nil {
				slog.Warn("quality_gate: retry run failed, accepting previous result",
					"delegation", task.ID, "error", rerunErr)
				result = currentResult
				break
			}
			currentResult = rerunResult
			result = currentResult
		}
	}

	return result, nil
}
