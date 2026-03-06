package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
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
}

func NewTeamsMethods(teamStore store.TeamStore, agentStore store.AgentStore, linkStore store.AgentLinkStore, agentRouter *agent.Router, msgBus *bus.MessageBus) *TeamsMethods {
	return &TeamsMethods{teamStore: teamStore, agentStore: agentStore, linkStore: linkStore, agentRouter: agentRouter, msgBus: msgBus}
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
	router.Register(protocol.MethodTeamsMembersAdd, m.handleAddMember)
	router.Register(protocol.MethodTeamsMembersRemove, m.handleRemoveMember)
	router.Register(protocol.MethodTeamsUpdate, m.handleUpdate)
	router.Register(protocol.MethodTeamsKnownUsers, m.handleKnownUsers)
}

// --- List ---

func (m *TeamsMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	ctx := context.Background()
	teams, err := m.teamStore.ListTeams(ctx)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
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

func (m *TeamsMethods) handleCreate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "teams not configured"))
		return
	}

	var params teamsCreateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "name is required"))
		return
	}
	if params.Lead == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "lead is required"))
		return
	}

	// Resolve lead agent
	leadAgent, err := resolveAgentInfo(m.agentStore, params.Lead)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "lead agent: "+err.Error()))
		return
	}

	// Resolve member agents
	var memberAgents []*store.AgentData
	for _, memberKey := range params.Members {
		ag, err := resolveAgentInfo(m.agentStore, memberKey)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "member agent "+memberKey+": "+err.Error()))
			return
		}
		memberAgents = append(memberAgents, ag)
	}

	ctx := context.Background()

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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to create team: "+err.Error()))
		return
	}

	// Add lead as member with lead role
	if err := m.teamStore.AddMember(ctx, team.ID, leadAgent.ID, store.TeamRoleLead); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to add lead as member: "+err.Error()))
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

	// Invalidate agent caches so TEAM.md gets injected
	if m.agentRouter != nil {
		m.agentRouter.InvalidateAgent(leadAgent.AgentKey)
		for _, ag := range memberAgents {
			m.agentRouter.InvalidateAgent(ag.AgentKey)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
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
