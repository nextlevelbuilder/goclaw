package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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
	taskPayload, ok := payload.(protocol.TeamTaskEventPayload)
	if !ok {
		bus.BroadcastForTenant(m.msgBus, name, store.TenantIDFromContext(ctx), payload)
		return
	}
	PublishTaskEventWithResolver(m.teamStore, m.msgBus, m.agentStore, name, taskPayload, uuid.Nil)
}

func PublishTaskEvent(teamStore store.TeamStore, publisher bus.EventPublisher, name string, payload protocol.TeamTaskEventPayload) bool {
	return PublishTaskEventWithResolver(teamStore, publisher, nil, name, payload, uuid.Nil)
}

// PublishTaskEventWithID keeps the legacy signature (no owner-display resolver).
// Owner display is left empty unless a resolver-aware caller is used.
func PublishTaskEventWithID(teamStore store.TeamStore, publisher bus.EventPublisher, name string, payload protocol.TeamTaskEventPayload, eventID uuid.UUID) bool {
	return PublishTaskEventWithResolver(teamStore, publisher, nil, name, payload, eventID)
}

type taskEventIdentityStore interface {
	GetTaskEventIdentity(context.Context, uuid.UUID) (uuid.UUID, uuid.UUID, string, error)
}

// AgentDisplayResolver resolves an owner agent key to its display name for
// authoritative task-event enrichment. store.AgentStore satisfies it, so the
// existing per-tenant agent lookup is reused instead of a bespoke query.
type AgentDisplayResolver interface {
	GetByKey(ctx context.Context, agentKey string) (*store.AgentData, error)
}

// PublishTaskEventWithResolver binds a globally unique logical event to the
// tenant stored on the task before any dashboard, audit, or notification
// fanout. The committed row is authoritative for identity/status/workflow/
// revision/owner key; the optional resolver supplies the owner display name
// from the authoritative agents lookup (never a stale caller value or UUID).
func PublishTaskEventWithResolver(teamStore store.TeamStore, publisher bus.EventPublisher, resolver AgentDisplayResolver, name string, payload protocol.TeamTaskEventPayload, eventID uuid.UUID) bool {
	if teamStore == nil || publisher == nil {
		return false
	}
	taskID, err := uuid.Parse(payload.TaskID)
	if err != nil {
		slog.Warn("team_task_event.invalid_task_id", "task_id", payload.TaskID, "event_type", name)
		return false
	}
	authoritativeCtx := store.WithCrossTenant(context.Background())
	task, err := teamStore.GetTask(authoritativeCtx, taskID)
	if err != nil || task == nil || task.TenantID == uuid.Nil {
		slog.Warn("team_task_event.authority_lookup_failed", "task_id", taskID, "event_type", name, "error", err)
		return false
	}
	if eventID == uuid.Nil {
		eventID = store.GenNewID()
	}
	tenantCtx := store.WithTenantID(context.Background(), task.TenantID)
	payload = enrichTaskEventPayloadFromAuthoritative(tenantCtx, resolver, payload, task, name)
	data := taskEventAuditData(name, payload)
	claim, err := teamStore.ClaimTaskEvent(tenantCtx, &store.TeamTaskEventData{
		ID: eventID, TaskID: task.ID, EventType: taskEventType(name), ActorType: payload.ActorType,
		ActorID: payload.ActorID, Data: data,
	})
	if err != nil {
		slog.Warn("team_task_event.identity_claim_failed", "tenant_id", task.TenantID, "event_id", eventID, "task_id", task.ID, "event_type", name, "error", err)
		return false
	}
	switch claim {
	case store.TaskEventClaimed:
		publisher.Broadcast(bus.Event{EventID: eventID, Name: name, TenantID: task.TenantID, Payload: payload})
		return true
	case store.TaskEventDuplicate:
		slog.Info("team_task_event.duplicate", "tenant_id", task.TenantID, "event_id", eventID, "task_id", task.ID, "event_type", name)
		return false
	default:
		attrs := []any{
			"attempted_tenant_id", task.TenantID, "event_id", eventID,
			"attempted_task_id", task.ID, "attempted_event_type", name,
		}
		if identityStore, ok := teamStore.(taskEventIdentityStore); ok {
			existingTenant, existingTask, existingType, lookupErr := identityStore.GetTaskEventIdentity(authoritativeCtx, eventID)
			if lookupErr == nil {
				attrs = append(attrs, "existing_tenant_id", existingTenant, "existing_task_id", existingTask, "existing_event_type", existingType)
			} else {
				attrs = append(attrs, "identity_lookup_error", lookupErr)
			}
		}
		slog.Warn("team_task_event.security_replay_rejected", attrs...)
		return false
	}
}

// PublishDeletedTaskEvent emits a dashboard tombstone from a task snapshot
// loaded before hard deletion. Hard deletion cascades task audit rows, so this
// path deliberately does not claim a new audit row after the task is gone.
func PublishDeletedTaskEvent(publisher bus.EventPublisher, task *store.TeamTaskData, payload protocol.TeamTaskEventPayload) bool {
	if publisher == nil || task == nil || task.ID == uuid.Nil || task.TenantID == uuid.Nil {
		return false
	}
	payload.TaskID = task.ID.String()
	payload.TeamID = task.TeamID.String()
	// A deleted task's owner is gone; the tombstone keeps only the row's
	// authoritative owner key (no display lookup on the deletion path).
	payload = enrichTaskEventPayloadFromAuthoritative(context.Background(), nil, payload, task, protocol.EventTeamTaskDeleted)
	eventID := store.GenNewID()
	publisher.Broadcast(bus.Event{
		EventID: eventID, Name: protocol.EventTeamTaskDeleted,
		TenantID: task.TenantID, Payload: payload,
	})
	slog.Info("team_task_event.delete_tombstone",
		"tenant_id", task.TenantID, "event_id", eventID,
		"task_id", task.ID, "event_type", protocol.EventTeamTaskDeleted)
	return true
}

// enrichTaskEventPayloadFromAuthoritative rebuilds the outbound payload so that
// every identity/status/workflow/revision/owner-key field comes from the
// committed DB row (never a stale request-time value), while preserving the
// caller-supplied routing/reason/progress/actor context. The owner display name
// is resolved via the authoritative agents lookup (resolver); on lookup failure
// the authoritative owner key is kept, the display is left empty, a warning is
// logged, and publication is NOT failed. The task row's OwnerAgentKey is the
// agent_key resolved via the agents JOIN (never a UUID), so an unassigned row
// clears any stale caller-supplied key. The resolution runs under the task's
// tenant context so the agent lookup is tenant-scoped.
func enrichTaskEventPayloadFromAuthoritative(ctx context.Context, resolver AgentDisplayResolver, payload protocol.TeamTaskEventPayload, task *store.TeamTaskData, eventType string) protocol.TeamTaskEventPayload {
	if task == nil {
		return payload
	}
	opts := TeamTaskEventOptions{
		Reason:          payload.Reason,
		UserID:          payload.UserID,
		Channel:         payload.Channel,
		ChatID:          payload.ChatID,
		PeerKind:        payload.PeerKind,
		LocalKey:        payload.LocalKey,
		CommentText:     payload.CommentText,
		ProgressPercent: payload.ProgressPercent,
		ProgressStep:    payload.ProgressStep,
		ActorType:       payload.ActorType,
		ActorID:         payload.ActorID,
	}
	ownerDisplay := resolveOwnerDisplayName(ctx, resolver, task, eventType)
	enriched := BuildTeamTaskEventPayload(task, ownerDisplay, opts)
	// Preserve an explicitly supplied timestamp (e.g. task creation time) rather
	// than the builder's time.Now(); zero value means "use now".
	if payload.Timestamp != "" {
		enriched.Timestamp = payload.Timestamp
	}
	return enriched
}

// resolveOwnerDisplayName looks up the committed owner key's display name via the
// authoritative agent store. It returns "" (never an error) so a failed lookup
// cannot break event publication; the failure is logged with tenant/team/task/
// owner identity for operators.
func resolveOwnerDisplayName(ctx context.Context, resolver AgentDisplayResolver, task *store.TeamTaskData, eventType string) string {
	if resolver == nil || task == nil || task.OwnerAgentKey == "" {
		return ""
	}
	ag, err := resolver.GetByKey(ctx, task.OwnerAgentKey)
	if err != nil || ag == nil {
		slog.Warn("team_task_event.owner_display_lookup_failed",
			"tenant_id", task.TenantID, "team_id", task.TeamID,
			"task_id", task.ID, "owner_agent_id", task.OwnerAgentID,
			"owner_agent_key", task.OwnerAgentKey,
			"event_type", eventType, "error", err)
		return ""
	}
	return ag.DisplayName
}

func taskEventType(name string) string {
	switch name {
	case protocol.EventTeamTaskCreated:
		return "created"
	case protocol.EventTeamTaskClaimed:
		return "claimed"
	case protocol.EventTeamTaskAssigned:
		return "assigned"
	case protocol.EventTeamTaskDispatched:
		return "dispatched"
	case protocol.EventTeamTaskCompleted:
		return "completed"
	case protocol.EventTeamTaskFailed:
		return "failed"
	case protocol.EventTeamTaskCancelled:
		return "cancelled"
	case protocol.EventTeamTaskReviewed:
		return "reviewed"
	case protocol.EventTeamTaskApproved:
		return "approved"
	case protocol.EventTeamTaskRejected:
		return "rejected"
	case protocol.EventTeamTaskCommented:
		return "commented"
	case protocol.EventTeamTaskProgress:
		return "progress"
	case protocol.EventTeamTaskUpdated:
		return "updated"
	case protocol.EventTeamTaskStale:
		return "stale"
	default:
		return name
	}
}

func taskEventAuditData(name string, payload protocol.TeamTaskEventPayload) json.RawMessage {
	var value any
	switch name {
	case protocol.EventTeamTaskFailed, protocol.EventTeamTaskRejected, protocol.EventTeamTaskCancelled:
		if payload.Reason != "" {
			value = map[string]string{"reason": payload.Reason}
		}
	case protocol.EventTeamTaskCommented:
		if payload.CommentText != "" {
			value = map[string]string{"comment_text": payload.CommentText}
		}
	case protocol.EventTeamTaskProgress:
		value = map[string]any{"progress_percent": payload.ProgressPercent, "progress_step": payload.ProgressStep}
	}
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	return raw
}

func reviewOutboundMessage(task *store.TeamTaskData, content string) bus.OutboundMessage {
	message := bus.OutboundMessage{
		Channel: task.Channel,
		ChatID:  task.ChatID,
		Content: content,
	}
	message.Metadata = TaskLocalKeyMetadata(task)
	return message
}

// TaskLocalKeyMetadata extracts local_key from task metadata for Telegram forum topic routing.
func TaskLocalKeyMetadata(task *store.TeamTaskData) map[string]string {
	if task == nil || task.Metadata == nil {
		return nil
	}
	if localKey, ok := task.Metadata[TaskMetaLocalKey].(string); ok && localKey != "" {
		return map[string]string{TaskMetaLocalKey: localKey}
	}
	return nil
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
// TeamToolBackend exported wrappers (helpers layer)
// ============================================================

func (m *TeamToolManager) BroadcastTeamEvent(ctx context.Context, name string, payload any) {
	m.broadcastTeamEvent(ctx, name, payload)
}
func (m *TeamToolManager) AgentDisplayName(ctx context.Context, key string) string {
	return m.agentDisplayName(ctx, key)
}
func (m *TeamToolManager) FollowupDelayMinutes(team *store.TeamData) int {
	return m.followupDelayMinutes(team)
}
func (m *TeamToolManager) FollowupMaxReminders(team *store.TeamData) int {
	return m.followupMaxReminders(team)
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

	m.broadcastTeamEvent(ctx, protocol.EventTeamTaskCreated, BuildTaskEventPayload(
		team.ID.String(), task.ID.String(),
		store.TeamTaskStatusPending,
		"", "",
		WithSubject(subject),
		WithContextInfo(ctx),
		WithTimestamp(task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")),
	))

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
	m.msgBus.PublishOutbound(reviewOutboundMessage(task, content))
}
