package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const teamCacheTTL = 5 * time.Minute

// teamCacheEntry wraps cached team data with a timestamp for TTL expiration.
type teamCacheEntry struct {
	team     *store.TeamData
	cachedAt time.Time
}

// TeamToolManager is the shared backend for team_tasks and team_message tools.
// It resolves the calling agent's team from context and provides access to
// the team store, agent store, and message bus.
// Includes a TTL cache for team data to avoid DB queries on every tool call.
type TeamToolManager struct {
	teamStore   store.TeamStore
	agentStore  store.AgentStore
	msgBus      *bus.MessageBus
	delegateMgr *DelegateManager // optional: enables delegation cancellation on task cancel
	teamCache   sync.Map         // agentID (uuid.UUID) → *teamCacheEntry
}

func NewTeamToolManager(teamStore store.TeamStore, agentStore store.AgentStore, msgBus *bus.MessageBus) *TeamToolManager {
	return &TeamToolManager{teamStore: teamStore, agentStore: agentStore, msgBus: msgBus}
}

// SetDelegateManager enables delegation cancellation when team tasks are cancelled.
func (m *TeamToolManager) SetDelegateManager(dm *DelegateManager) {
	m.delegateMgr = dm
}

// resolveTeam returns the team that the calling agent belongs to.
// Uses a TTL cache to avoid repeated DB queries. Access control
// (user/channel) is checked on every call regardless of cache hit.
func (m *TeamToolManager) resolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error) {
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("no agent context — team tools require database stores")
	}

	// Check cache first
	if entry, ok := m.teamCache.Load(agentID); ok {
		ce := entry.(*teamCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			// Cache hit — still check access (user/channel vary per call)
			userID := store.UserIDFromContext(ctx)
			channel := ToolChannelFromCtx(ctx)
			if err := checkTeamAccess(ce.team.Settings, userID, channel); err != nil {
				return nil, uuid.Nil, err
			}
			return ce.team, agentID, nil
		}
		m.teamCache.Delete(agentID) // expired
	}

	// Cache miss → DB
	team, err := m.teamStore.GetTeamForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("workspace: resolveTeam DB error", "agent_id", agentID, "error", err)
		return nil, uuid.Nil, fmt.Errorf("failed to resolve team: %w", err)
	}
	if team == nil {
		slog.Warn("workspace: agent has no team", "agent_id", agentID)
		return nil, uuid.Nil, fmt.Errorf("this agent is not part of any team")
	}

	// Store in cache
	m.teamCache.Store(agentID, &teamCacheEntry{team: team, cachedAt: time.Now()})

	// Check access
	userID := store.UserIDFromContext(ctx)
	channel := ToolChannelFromCtx(ctx)
	if err := checkTeamAccess(team.Settings, userID, channel); err != nil {
		return nil, uuid.Nil, err
	}

	return team, agentID, nil
}

// requireLead checks if the calling agent is the team lead.
// Delegate/system channels bypass this check (they act on behalf of the lead).
func (m *TeamToolManager) requireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error {
	channel := ToolChannelFromCtx(ctx)
	if channel == ChannelDelegate || channel == ChannelSystem {
		return nil
	}
	if agentID != team.LeadAgentID {
		return fmt.Errorf("only the team lead can perform this action")
	}
	return nil
}

// InvalidateTeam clears all cached team data.
// Called when team membership, settings, or links change.
// Full clear is acceptable because team mutations are rare (admin-initiated).
func (m *TeamToolManager) InvalidateTeam() {
	m.teamCache.Range(func(k, _ any) bool { m.teamCache.Delete(k); return true })
}

// resolveAgentByKey looks up an agent by key and returns its UUID.
func (m *TeamToolManager) resolveAgentByKey(key string) (uuid.UUID, error) {
	ag, err := m.agentStore.GetByKey(context.Background(), key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return ag.ID, nil
}

// agentKeyFromID returns the agent_key for a given UUID.
func (m *TeamToolManager) agentKeyFromID(ctx context.Context, id uuid.UUID) string {
	ag, err := m.agentStore.GetByID(ctx, id)
	if err != nil {
		return id.String()
	}
	return ag.AgentKey
}

// broadcastTeamEvent sends a real-time event via the message bus for team activity visibility.
func (m *TeamToolManager) broadcastTeamEvent(name string, payload any) {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    name,
		Payload: payload,
	})
}

// resolveTeamRole returns the calling agent's role in the team.
// Unlike requireLead(), this does NOT bypass for delegate channel —
// workspace RBAC must respect actual roles even during delegation.
func (m *TeamToolManager) resolveTeamRole(ctx context.Context, team *store.TeamData, agentID uuid.UUID) (string, error) {
	if agentID == team.LeadAgentID {
		return store.TeamRoleLead, nil
	}
	members, err := m.teamStore.ListMembers(ctx, team.ID)
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
	ag, err := m.agentStore.GetByKey(ctx, key)
	if err != nil || ag.DisplayName == "" {
		return ""
	}
	return ag.DisplayName
}

// ============================================================
// Version helpers
// ============================================================

// IsTeamV2 checks if team has version >= 2 in settings.
// Returns false for nil team, nil/empty settings, or version < 2.
func IsTeamV2(team *store.TeamData) bool {
	if team == nil || team.Settings == nil {
		return false
	}
	var s struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(team.Settings, &s) != nil {
		return false
	}
	return s.Version >= 2
}

// ============================================================
// Follow-up settings helpers
// ============================================================

const (
	defaultFollowupDelayMinutes = 30
	defaultFollowupMaxReminders = 0 // 0 = unlimited
)

// followupDelayMinutes returns the team's followup_interval_minutes setting, or the default.
// Returns 0 for v1 teams (followup disabled).
func (m *TeamToolManager) followupDelayMinutes(team *store.TeamData) int {
	if !IsTeamV2(team) {
		return 0
	}
	if team.Settings == nil {
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
// Returns 0 for v1 teams (followup disabled).
func (m *TeamToolManager) followupMaxReminders(team *store.TeamData) int {
	if !IsTeamV2(team) {
		return 0
	}
	if team.Settings == nil {
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
// Returns EscalationNone for v1 teams.
func (m *TeamToolManager) checkEscalation(team *store.TeamData, action string) EscalationResult {
	if !IsTeamV2(team) {
		return EscalationNone
	}
	if team.Settings == nil {
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

	m.broadcastTeamEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
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
