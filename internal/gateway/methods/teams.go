package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TeamsMethods handles teams.* RPC methods.
type TeamsMethods struct {
	teamStore   store.TeamStore
	agentStore  store.AgentStore
	linkStore   store.AgentLinkStore // for auto-creating bidirectional links
	agentRouter *agent.Router        // for cache invalidation
	msgBus      *bus.MessageBus      // for pub/sub cache invalidation
	eventBus    bus.EventPublisher
	dataDir string // workspace data directory for resolving file paths
}

func NewTeamsMethods(teamStore store.TeamStore, agentStore store.AgentStore, linkStore store.AgentLinkStore, agentRouter *agent.Router, msgBus *bus.MessageBus, eventBus bus.EventPublisher, dataDir string) *TeamsMethods {
	return &TeamsMethods{teamStore: teamStore, agentStore: agentStore, linkStore: linkStore, agentRouter: agentRouter, msgBus: msgBus, eventBus: eventBus, dataDir: dataDir}
}

// emitTeamCacheInvalidate broadcasts a cache invalidation event for team data.
func (m *TeamsMethods) emitTeamCacheInvalidate() {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindTeam},
	})
}

func (m *TeamsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodTeamsList, m.handleList)
	router.Register(protocol.MethodTeamsCreate, m.handleCreate)
	router.Register(protocol.MethodTeamsGet, m.handleGet)
	router.Register(protocol.MethodTeamsDelete, m.handleDelete)
	router.Register(protocol.MethodTeamsTaskList, m.handleTaskList)
	router.Register(protocol.MethodTeamsTaskActiveBySession, m.handleTaskActiveBySession)
	router.Register(protocol.MethodTeamsTaskApprove, m.handleTaskApprove)
	router.Register(protocol.MethodTeamsTaskReject, m.handleTaskReject)
	router.Register(protocol.MethodTeamsMembersAdd, m.handleAddMember)
	router.Register(protocol.MethodTeamsMembersRemove, m.handleRemoveMember)
	router.Register(protocol.MethodTeamsUpdate, m.handleUpdate)
	router.Register(protocol.MethodTeamsKnownUsers, m.handleKnownUsers)
	router.Register(protocol.MethodTeamsScopes, m.handleScopes)

	// Workspace handlers
	m.RegisterWorkspace(router)

	// Events handlers
	router.Register(protocol.MethodTeamsEventsList, m.handleEventsList)

	// Task detail handlers
	m.RegisterTasks(router)
}

// --- List ---

func (m *TeamsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var teams []store.TeamData
	var err error
	if permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		teams, err = m.teamStore.ListTeams(ctx)
	} else {
		callerID := store.UserIDFromContext(ctx)
		teams, err = m.teamStore.ListUserTeams(ctx, callerID)
	}
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"teams": teams,
		"count": len(teams),
	}))
}

// --- Create ---

type teamsCreateParams struct {
	Name        string          `json:"name"`
	Lead        string          `json:"lead"`    // agent key or UUID
	Members     []string        `json:"members"` // agent keys or UUIDs
	Description string          `json:"description"`
	Settings    json.RawMessage `json:"settings"`
}

func (m *TeamsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsCreateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if params.Lead == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "lead")))
		return
	}

	// Resolve lead agent
	leadAgent, err := resolveAgentInfo(ctx, m.agentStore, params.Lead)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "lead agent: "+err.Error()))
		return
	}

	// Enforce single-team leadership: an agent can only lead one team.
	if existingTeam, _ := m.teamStore.GetTeamForAgent(ctx, leadAgent.ID); existingTeam != nil && existingTeam.LeadAgentID == leadAgent.ID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			fmt.Sprintf("agent %q already leads team %q — each agent can only lead one team", params.Lead, existingTeam.Name)))
		return
	}

	// Resolve member agents
	var memberAgents []*store.AgentData
	for _, memberKey := range params.Members {
		ag, err := resolveAgentInfo(ctx, m.agentStore, memberKey)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "member agent "+memberKey+": "+err.Error()))
			return
		}
		memberAgents = append(memberAgents, ag)
	}

	// Create team
	team := &store.TeamData{
		Name:        params.Name,
		LeadAgentID: leadAgent.ID,
		Description: params.Description,
		Status:      store.TeamStatusActive,
		Settings:    params.Settings,
		CreatedBy:   client.UserID(),
	}
	if err := m.teamStore.CreateTeam(ctx, team); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "team", err.Error())))
		return
	}

	// Add lead as member with lead role
	if err := m.teamStore.AddMember(ctx, team.ID, leadAgent.ID, store.TeamRoleLead); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "team lead membership", err.Error())))
		return
	}

	// Add members
	for _, ag := range memberAgents {
		if ag.ID == leadAgent.ID {
			continue // lead already added
		}
		if err := m.teamStore.AddMember(ctx, team.ID, ag.ID, store.TeamRoleMember); err != nil {
			slog.Warn("teams.create: failed to add member", "agent", ag.AgentKey, "error", err)
		}
	}

	// Auto-create outbound agent_links from lead to each member.
	// Only the lead can delegate to members.
	if m.linkStore != nil {
		m.autoCreateTeamLinks(ctx, team.ID, leadAgent, memberAgents, client.UserID())
	}

	// Invalidate agent + team tool caches so TEAM.md gets injected
	m.invalidateTeamCaches(ctx, team.ID)

	emitAudit(m.eventBus, client, "team.created", "team", team.ID.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"team": team,
	}))

	// Emit team.created event
	if m.msgBus != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamCreated,
			Payload: protocol.TeamCreatedPayload{
				TeamID:          team.ID.String(),
				TeamName:        params.Name,
				LeadAgentKey:    leadAgent.AgentKey,
				LeadDisplayName: leadAgent.DisplayName,
				MemberCount:     len(memberAgents) + 1,
			},
		})
	}
}
