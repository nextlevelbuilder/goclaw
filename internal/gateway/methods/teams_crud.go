package methods

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- Get ---

type teamsGetParams struct {
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleGet(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	var params teamsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "teamId is required"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid teamId"))
		return
	}

	ctx := context.Background()
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	members, err := m.teamStore.ListMembers(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"team":    team,
		"members": members,
	}))
}

// --- Delete ---

type teamsDeleteParams struct {
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleDelete(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	var params teamsDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "teamId is required"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid teamId"))
		return
	}

	ctx := context.Background()

	// Fetch team and members before deleting for event + cache invalidation
	team, _ := m.teamStore.GetTeam(ctx, teamID)
	members, _ := m.teamStore.ListMembers(ctx, teamID)

	if err := m.teamStore.DeleteTeam(ctx, teamID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to delete team: "+err.Error()))
		return
	}

	// Invalidate agent caches
	if m.agentRouter != nil {
		for _, member := range members {
			m.agentRouter.InvalidateAgent(member.AgentKey)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{"ok": true}))

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
	TeamID string `json:"teamId"`
}

func (m *TeamsMethods) handleTaskList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	var params teamsTaskListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "teamId is required"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid teamId"))
		return
	}

	ctx := context.Background()
	tasks, err := m.teamStore.ListTasks(ctx, teamID, "newest", store.TeamTaskFilterAll, "")
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	}))
}

// --- Update (settings) ---

type teamsUpdateParams struct {
	TeamID   string                 `json:"teamId"`
	Settings map[string]interface{} `json:"settings"`
}

func (m *TeamsMethods) handleUpdate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	var params teamsUpdateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "teamId is required"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid teamId"))
		return
	}

	ctx := context.Background()

	// Validate team exists
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "team not found: "+err.Error()))
		return
	}

	// Validate settings against teamAccessSettings schema (strip unknown fields)
	type teamAccessSettings struct {
		AllowUserIDs          []string `json:"allow_user_ids"`
		DenyUserIDs           []string `json:"deny_user_ids"`
		AllowChannels         []string `json:"allow_channels"`
		DenyChannels          []string `json:"deny_channels"`
		ProgressNotifications *bool    `json:"progress_notifications,omitempty"`
	}
	raw, _ := json.Marshal(params.Settings)
	var access teamAccessSettings
	if err := json.Unmarshal(raw, &access); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid settings: "+err.Error()))
		return
	}
	cleaned, _ := json.Marshal(access)

	updates := map[string]any{"settings": json.RawMessage(cleaned)}
	if err := m.teamStore.UpdateTeam(ctx, teamID, updates); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to update team: "+err.Error()))
		return
	}

	m.invalidateTeamCaches(ctx, teamID)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{"ok": true}))

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

func (m *TeamsMethods) handleKnownUsers(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	var params teamsKnownUsersParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "teamId is required"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid teamId"))
		return
	}

	ctx := context.Background()
	users, err := m.teamStore.KnownUserIDs(ctx, teamID, 100)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"users": users,
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
