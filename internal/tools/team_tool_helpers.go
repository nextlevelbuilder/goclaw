package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// broadcastTeamEvent sends a real-time event via the message bus for team activity visibility.
// Includes tenant_id from context for proper WS event filtering.
func (m *TeamToolManager) broadcastTeamEvent(ctx context.Context, name string, payload any) {
	if m.msgBus == nil {
		return
	}
	bus.BroadcastForTenant(m.msgBus, name, store.TenantIDFromContext(ctx), payload)
}

// resolveTeamRole returns the calling agent's role in the team.
// Unlike requireLead(), this does NOT bypass for teammate channel —
// workspace RBAC must respect actual roles even for teammate agents.
func (m *TeamToolManager) resolveTeamRole(ctx context.Context, team *store.TeamData, agentID uuid.UUID) (string, error) {
	if agentID == team.LeadAgentID {
		return store.TeamRoleLead, nil
	}
	members, err := m.cachedListMembers(ctx, team.ID, agentID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve team role: %w", err)
	}
	for _, member := range members {
		if member.AgentID == agentID {
			return member.Role, nil
		}
	}
	return "", fmt.Errorf("agent is not a member of this team")
}

// agentDisplayName returns the display name for an agent key, falling back to empty string.
func (m *TeamToolManager) agentDisplayName(ctx context.Context, key string) string {
	ag, err := m.cachedGetAgentByKey(ctx, key)
	if err != nil || ag.DisplayName == "" {
		return ""
	}
	return ag.DisplayName
}

// ============================================================
// Version helpers
// ============================================================


// ============================================================
// Follow-up settings helpers
// ============================================================

const (
	defaultFollowupDelayMinutes = 30
	defaultFollowupMaxReminders = 0 // 0 = unlimited
)

// followupDelayMinutes returns the team's followup_interval_minutes setting, or the default.
func (m *TeamToolManager) followupDelayMinutes(team *store.TeamData) int {
	if team == nil || team.Settings == nil {
		return defaultFollowupDelayMinutes
	}
	var settings map[string]any
	if json.Unmarshal(team.Settings, &settings) != nil {
		return defaultFollowupDelayMinutes
	}
	if v, ok := settings["followup_interval_minutes"].(float64); ok && v > 0 {
		return int(v)
	}
	return defaultFollowupDelayMinutes
}

// followupMaxReminders returns the team's followup_max_reminders setting, or the default.
func (m *TeamToolManager) followupMaxReminders(team *store.TeamData) int {
	if team == nil || team.Settings == nil {
		return defaultFollowupMaxReminders
	}
	var settings map[string]any
	if json.Unmarshal(team.Settings, &settings) != nil {
		return defaultFollowupMaxReminders
	}
	if v, ok := settings["followup_max_reminders"].(float64); ok && v >= 0 {
		return int(v)
	}
	return defaultFollowupMaxReminders
}

// ============================================================
// Escalation policy
// ============================================================

// EscalationResult indicates how an action should be handled.
type EscalationResult int

const (
	EscalationNone   EscalationResult = iota // no escalation configured
	EscalationAuto                           // LLM chooses (currently: always review)
	EscalationReview                         // create review task
	EscalationReject                         // reject outright
)

// checkEscalation parses the team's escalation_mode and escalation_actions settings.
func (m *TeamToolManager) checkEscalation(team *store.TeamData, action string) EscalationResult {
	if team == nil || team.Settings == nil {
		return EscalationNone
	}
	var settings map[string]any
	if err := json.Unmarshal(team.Settings, &settings); err != nil {
		return EscalationNone
	}

	mode, _ := settings["escalation_mode"].(string)
	if mode == "" {
		return EscalationNone
	}

	// Check if action is in escalation_actions list.
	actionsRaw, _ := settings["escalation_actions"].([]any)
	if len(actionsRaw) > 0 {
		found := false
		for _, a := range actionsRaw {
			if s, ok := a.(string); ok && s == action {
				found = true
				break
			}
		}
		if !found {
			return EscalationNone
		}
	}

	switch mode {
	case "auto":
		return EscalationAuto
	case "review":
		return EscalationReview
	case "reject":
		return EscalationReject
	default:
		return EscalationNone
	}
}

// createEscalationTask creates an escalation task and broadcasts the event.
func (m *TeamToolManager) createEscalationTask(ctx context.Context, team *store.TeamData, agentID uuid.UUID, subject, description string) *Result {
	task := &store.TeamTaskData{
		TeamID:           team.ID,
		Subject:          subject,
		Description:      description,
		Status:           store.TeamTaskStatusPending,
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		TaskType:         "escalation",
		CreatedByAgentID: &agentID,
		ChatID:           ToolChatIDFromCtx(ctx),
	}
	if err := m.teamStore.CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create escalation task: " + err.Error())
	}

	m.broadcastTeamEvent(ctx, protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    task.ID.String(),
		Subject:   subject,
		Status:    store.TeamTaskStatusPending,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})

	// Notify channel if possible.
	m.notifyChannelReview(task)

	return NewResult(fmt.Sprintf("Action requires approval. Escalation task created: %s (id=%s). A human must approve before this action can proceed.", subject, task.Identifier))
}

// notifyChannelReview publishes an outbound message to the origin channel about a pending review.
func (m *TeamToolManager) notifyChannelReview(task *store.TeamTaskData) {
	if m.msgBus == nil || task.Channel == "" || task.ChatID == "" {
		return
	}
	content := fmt.Sprintf("🔔 Escalation: \"%s\" requires human review (task %s).", task.Subject, task.Identifier)
	m.msgBus.PublishOutbound(bus.OutboundMessage{
		Channel: task.Channel,
		ChatID:  task.ChatID,
		Content: content,
	})
}
