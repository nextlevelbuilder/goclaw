package methods

import (
	"context"
	"encoding/json"


	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- Add Member ---

type teamsAddMemberParams struct {
	TeamID string `json:"teamId"`
	Agent  string `json:"agent"` // agent key or UUID
	Role   string `json:"role"`  // "member" (default) or "reviewer"
}

func (m *TeamsMethods) handleAddMember(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsAddMemberParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" || params.Agent == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId and agent")))
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

	// Resolve agent
	ag, err := resolveAgentInfo(ctx, m.agentStore, params.Agent)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "agent: "+err.Error()))
		return
	}

	// Prevent adding lead again
	if ag.ID == team.LeadAgentID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAgentIsTeamLead)))
		return
	}

	// Validate and default role
	role := params.Role
	switch role {
	case store.TeamRoleMember, store.TeamRoleReviewer:
		// valid
	case "":
		role = store.TeamRoleMember
	default:
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "role must be member or reviewer"))
		return
	}

	// Add member
	if err := m.teamStore.AddMember(ctx, teamID, ag.ID, role); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "member", err.Error())))
		return
	}

	// Invalidate caches for all team members
	m.invalidateTeamCaches(ctx, teamID)
	emitAudit(m.eventBus, client, "team.member_added", "team", teamID.String())

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Emit team.member.added event
	if m.msgBus != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamMemberAdded,
			Payload: protocol.TeamMemberAddedPayload{
				TeamID:      teamID.String(),
				TeamName:    team.Name,
				AgentID:     ag.ID.String(),
				AgentKey:    ag.AgentKey,
				DisplayName: ag.DisplayName,
				Role:        role,
			},
		})
	}
}

// --- Remove Member ---

type teamsRemoveMemberParams struct {
	TeamID  string `json:"teamId"`
	AgentID string `json:"agentId"` // agent UUID
}

func (m *TeamsMethods) handleRemoveMember(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsRemoveMemberParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" || params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "teamId and agentId")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agentId")))
		return
	}

	// Validate team exists and prevent removing the lead
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgNotFound, "team", err.Error())))
		return
	}
	if agentID == team.LeadAgentID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCannotRemoveTeamLead)))
		return
	}

	// Fetch agent info before removal for event emission
	removedAgent, _ := m.agentStore.GetByID(ctx, agentID)

	// Remove member
	if err := m.teamStore.RemoveMember(ctx, teamID, agentID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "member", err.Error())))
		return
	}

	// Invalidate caches for all remaining members + removed agent
	m.invalidateTeamCaches(ctx, teamID)
	if m.agentRouter != nil {
		ag, err := m.agentStore.GetByID(ctx, agentID)
		if err == nil {
			m.agentRouter.InvalidateAgent(ag.AgentKey)
		}
	}

	emitAudit(m.eventBus, client, "team.member_removed", "team", teamID.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Emit team.member.removed event
	if m.msgBus != nil && removedAgent != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamMemberRemoved,
			Payload: protocol.TeamMemberRemovedPayload{
				TeamID:      teamID.String(),
				TeamName:    team.Name,
				AgentID:     removedAgent.ID.String(),
				AgentKey:    removedAgent.AgentKey,
				DisplayName: removedAgent.DisplayName,
			},
		})
	}
}

