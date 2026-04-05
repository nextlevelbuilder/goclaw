package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- Get ---

type teamsGetParams struct {
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Non-admin callers must be a member of the team to view it.
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(ctx)
		if ok, err := m.teamStore.HasTeamAccess(ctx, teamID, callerID); err != nil || !ok {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "team", params.TeamID)))
			return
		}
	}

	members, err := m.teamStore.ListMembers(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"team":    team,
		"members": members,
	}))
}

// --- Delete ---

type teamsDeleteParams struct {
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	// Non-admin callers must have team access to delete.
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(ctx)
		if ok, err := m.teamStore.HasTeamAccess(ctx, teamID, callerID); err != nil || !ok {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "team", params.TeamID)))
			return
		}
	}

	// Fetch team and members before deleting for event + cache invalidation
	team, _ := m.teamStore.GetTeam(ctx, teamID)
	members, _ := m.teamStore.ListMembers(ctx, teamID)

	if err := m.teamStore.DeleteTeam(ctx, teamID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "team", err.Error())))
		return
	}

	// Invalidate agent caches
	if m.agentRouter != nil {
		for _, member := range members {
			m.agentRouter.InvalidateAgent(member.AgentKey)
		}
	}

	emitAudit(m.eventBus, client, "team.deleted", "team", teamID.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Emit team.deleted event
	if m.msgBus != nil && team != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamDeleted,
			Payload: protocol.TeamDeletedPayload{
				TeamID:   teamID.String(),
				TeamName: team.Name,
			},
		})
	}
}

// --- Task List (admin view) ---

type teamsTaskListParams struct {
	TeamID  string `json:"teamId"`
	Status  string `json:"status"`  // "" = active, "completed", "all"
	Channel string `json:"channel"` // scope filter
	ChatID  string `json:"chatId"`  // scope filter
}

func (m *TeamsMethods) handleTaskList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsTaskListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	const dashboardLimit = 200
	tasks, err := m.teamStore.ListTasks(ctx, teamID, "newest", params.Status, "", params.Channel, params.ChatID, dashboardLimit, 0)
	if err != nil {
		slog.Warn("teams.tasks.list failed", "team_id", teamID, "status_filter", params.Status, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// ListTasks returns limit+1 rows for hasMore detection; truncate for dashboard response.
	hasMore := len(tasks) > dashboardLimit
	if hasMore {
		tasks = tasks[:dashboardLimit]
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	}))
}

// --- Active Tasks by Session ---

func (m *TeamsMethods) handleTaskActiveBySession(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params struct {
		SessionKey string `json:"sessionKey"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.SessionKey == "" {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"tasks": []any{}}))
		return
	}

	tasks, err := m.teamStore.ListActiveTasksByChatID(ctx, params.SessionKey)
	if err != nil {
		slog.Warn("teams.tasks.active-by-session failed", "session_key", params.SessionKey, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Map to lightweight ActiveTeamTask shape for the sidebar
	type activeTask struct {
		TaskID           string `json:"taskId"`
		TaskNumber       int    `json:"taskNumber"`
		Subject          string `json:"subject"`
		Status           string `json:"status"`
		OwnerAgentKey    string `json:"ownerAgentKey,omitempty"`
		OwnerDisplayName string `json:"ownerDisplayName,omitempty"`
		ProgressPercent  int    `json:"progressPercent,omitempty"`
		ProgressStep     string `json:"progressStep,omitempty"`
		CommentCount     int    `json:"commentCount,omitempty"`
		AttachmentCount  int    `json:"attachmentCount,omitempty"`
	}

	result := make([]activeTask, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, activeTask{
			TaskID:          t.ID.String(),
			TaskNumber:      t.TaskNumber,
			Subject:         t.Subject,
			Status:          t.Status,
			OwnerAgentKey:   t.OwnerAgentKey,
			ProgressPercent: t.ProgressPercent,
			ProgressStep:    t.ProgressStep,
			CommentCount:    t.CommentCount,
			AttachmentCount: t.AttachmentCount,
		})
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"tasks": result,
	}))
}

// --- Update (settings) ---

type teamsUpdateParams struct {
	TeamID      string         `json:"teamId"`
	Name        string         `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Settings    map[string]any `json:"settings"`
}

func (m *TeamsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsUpdateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	// Validate team exists
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgNotFound, "team", err.Error())))
		return
	}

	// Validate settings against teamAccessSettings schema (strip unknown fields).
	// CRITICAL: version field MUST be here — otherwise it gets stripped on save.
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
		WorkspaceScope        string   `json:"workspace_scope,omitempty"`
		WorkspaceQuotaMB      *int     `json:"workspace_quota_mb,omitempty"`
		Notifications         *struct {
			Dispatched *bool  `json:"dispatched,omitempty"`
			Progress   *bool  `json:"progress,omitempty"`
			Failed     *bool  `json:"failed,omitempty"`
			Completed  *bool  `json:"completed,omitempty"`
			Commented  *bool  `json:"commented,omitempty"`
			NewTask    *bool  `json:"new_task,omitempty"`
			Mode       string `json:"mode,omitempty"`
		} `json:"notifications,omitempty"`
		MemberRequests *struct {
			Enabled      *bool `json:"enabled,omitempty"`
			AutoDispatch *bool `json:"auto_dispatch,omitempty"`
		} `json:"member_requests,omitempty"`
	}
	raw, _ := json.Marshal(params.Settings)
	var access teamAccessSettings
	if err := json.Unmarshal(raw, &access); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}
	cleaned, _ := json.Marshal(access)

	updates := map[string]any{"settings": json.RawMessage(cleaned)}
	if params.Name != "" {
		updates["name"] = params.Name
	}
	if params.Description != nil {
		updates["description"] = *params.Description
	}
	if err := m.teamStore.UpdateTeam(ctx, teamID, updates); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "team", err.Error())))
		return
	}

	m.invalidateTeamCaches(ctx, teamID)
	emitAudit(m.eventBus, client, "team.updated", "team", teamID.String())

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Emit team.updated event
	if m.msgBus != nil {
		changes := make([]string, 0, len(updates))
		for k := range updates {
			changes = append(changes, k)
		}
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamUpdated,
			Payload: protocol.TeamUpdatedPayload{
				TeamID:   teamID.String(),
				TeamName: team.Name,
				Changes:  changes,
			},
		})
	}
}

// --- Known Users ---

type teamsKnownUsersParams struct {
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleKnownUsers(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsKnownUsersParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	users, err := m.teamStore.KnownUserIDs(ctx, teamID, 100)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"users": users,
	}))
}

// --- Scopes ---

type teamsScopesParams struct {
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleScopes(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsScopesParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	scopes, err := m.teamStore.ListTaskScopes(ctx, teamID)
	if err != nil {
		slog.Warn("teams.scopes failed", "team_id", teamID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"scopes": scopes,
	}))
}

// --- Events ---

type teamsEventsListParams struct {
	TeamID string `json:"team_id"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (m *TeamsMethods) handleEventsList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsEventsListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "team_id")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	events, err := m.teamStore.ListTeamEvents(ctx, teamID, params.Limit, params.Offset)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}
	if events == nil {
		events = []store.TeamTaskEventData{}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"events": events,
		"count":  len(events),
	}))
}

// invalidateTeamCaches invalidates agent caches for all members of a team
// and emits a pub/sub event for TeamToolManager cache invalidation.
func (m *TeamsMethods) invalidateTeamCaches(ctx context.Context, teamID uuid.UUID) {
	if m.agentRouter != nil {
		members, err := m.teamStore.ListMembers(ctx, teamID)
		if err == nil {
			for _, member := range members {
				if member.AgentKey != "" {
					m.agentRouter.InvalidateAgent(member.AgentKey)
				}
			}
		}
	}
	m.emitTeamCacheInvalidate()
}
