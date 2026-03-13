package methods

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// AgentLinksMethods handles agents.links.* RPC methods.
type AgentLinksMethods struct {
	linkStore   store.AgentLinkStore
	agentStore  store.AgentStore
	agentRouter *agent.Router   // for cache invalidation when links change
	msgBus      *bus.MessageBus // for pub/sub cache invalidation
	eventBus    bus.EventPublisher
}

func NewAgentLinksMethods(linkStore store.AgentLinkStore, agentStore store.AgentStore, agentRouter *agent.Router, msgBus *bus.MessageBus, eventBus bus.EventPublisher) *AgentLinksMethods {
	return &AgentLinksMethods{linkStore: linkStore, agentStore: agentStore, agentRouter: agentRouter, msgBus: msgBus, eventBus: eventBus}
}

func (m *AgentLinksMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodAgentsLinksList, m.handleList)
	router.Register(protocol.MethodAgentsLinksCreate, m.handleCreate)
	router.Register(protocol.MethodAgentsLinksUpdate, m.handleUpdate)
	router.Register(protocol.MethodAgentsLinksDelete, m.handleDelete)
}

// --- List ---

type linksListParams struct {
	AgentID   string `json:"agentId"`
	Direction string `json:"direction"` // "from" (default), "to", "all"
}

func (m *AgentLinksMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.linkStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgLinksNotConfigured)))
		return
	}

	var params linksListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}

	agentID, err := resolveAgentUUID(m.agentStore, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	var links []store.AgentLinkData

	switch params.Direction {
	case "to":
		links, err = m.linkStore.ListLinksTo(ctx, agentID)
	case "all":
		from, errFrom := m.linkStore.ListLinksFrom(ctx, agentID)
		to, errTo := m.linkStore.ListLinksTo(ctx, agentID)
		if errFrom != nil {
			err = errFrom
		} else if errTo != nil {
			err = errTo
		} else {
			links = append(from, to...)
		}
	default: // "from"
		links, err = m.linkStore.ListLinksFrom(ctx, agentID)
	}

	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"links": links,
		"count": len(links),
	}))
}

// --- Create ---

type linksCreateParams struct {
	SourceAgent   string          `json:"sourceAgent"`
	TargetAgent   string          `json:"targetAgent"`
	Direction     string          `json:"direction"`
	Description   string          `json:"description"`
	MaxConcurrent int             `json:"maxConcurrent"`
	Settings      json.RawMessage `json:"settings"`
}

func (m *AgentLinksMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.linkStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgLinksNotConfigured)))
		return
	}

	var params linksCreateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.SourceAgent == "" || params.TargetAgent == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "sourceAgent and targetAgent")))
		return
	}

	direction := params.Direction
	if direction == "" {
		direction = store.LinkDirectionOutbound
	}
	if direction != store.LinkDirectionOutbound && direction != store.LinkDirectionInbound && direction != store.LinkDirectionBidirectional {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidDirection)))
		return
	}

	sourceAgent, err := resolveAgentInfo(m.agentStore, params.SourceAgent)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "source agent: "+err.Error()))
		return
	}
	targetAgent, err := resolveAgentInfo(m.agentStore, params.TargetAgent)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "target agent: "+err.Error()))
		return
	}

	if sourceAgent.ID == targetAgent.ID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSourceTargetSame)))
		return
	}

	// Delegation targets must be predefined agents (open agents have no agent-level context files)
	if targetAgent.AgentType == store.AgentTypeOpen {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCannotDelegateOpen)))
		return
	}

	maxConcurrent := params.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}

	link := &store.AgentLinkData{
		SourceAgentID: sourceAgent.ID,
		TargetAgentID: targetAgent.ID,
		Direction:     direction,
		Description:   params.Description,
		MaxConcurrent: maxConcurrent,
		Settings:      params.Settings,
		Status:        store.LinkStatusActive,
		CreatedBy:     client.UserID(),
	}

	if err := m.linkStore.CreateLink(ctx, link); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "link", err.Error())))
		return
	}

	// Invalidate agent cache so DELEGATION.md gets regenerated with new link
	if m.agentRouter != nil {
		m.agentRouter.InvalidateAgent(sourceAgent.AgentKey)
		m.agentRouter.InvalidateAgent(targetAgent.AgentKey)
	}
	m.emitTeamCacheInvalidate()
	emitAudit(m.eventBus, client, "agent_link.created", "agent_link", link.ID.String())

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"link": link,
	}))

	// Emit agent_link.created event
	if m.msgBus != nil {
		payload := protocol.AgentLinkCreatedPayload{
			LinkID:         link.ID.String(),
			SourceAgentID:  sourceAgent.ID.String(),
			SourceAgentKey: sourceAgent.AgentKey,
			TargetAgentID:  targetAgent.ID.String(),
			TargetAgentKey: targetAgent.AgentKey,
			Direction:      direction,
			Status:         store.LinkStatusActive,
		}
		if link.TeamID != nil {
			payload.TeamID = link.TeamID.String()
		}
		m.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventAgentLinkCreated,
			Payload: payload,
		})
	}
}

// --- Update ---

type linksUpdateParams struct {
	LinkID        string          `json:"linkId"`
	Direction     string          `json:"direction"`
	Description   *string         `json:"description"`
	MaxConcurrent *int            `json:"maxConcurrent"`
	Settings      json.RawMessage `json:"settings"`
	Status        string          `json:"status"`
}

func (m *AgentLinksMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.linkStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgLinksNotConfigured)))
		return
	}

	var params linksUpdateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.LinkID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "linkId")))
		return
	}

	linkID, err := uuid.Parse(params.LinkID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "linkId")))
		return
	}

	updates := map[string]any{}
	if params.Direction != "" {
		if params.Direction != store.LinkDirectionOutbound && params.Direction != store.LinkDirectionInbound && params.Direction != store.LinkDirectionBidirectional {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidDirection)))
			return
		}
		updates["direction"] = params.Direction
	}
	if params.Description != nil {
		updates["description"] = *params.Description
	}
	if params.MaxConcurrent != nil && *params.MaxConcurrent > 0 {
		updates["max_concurrent"] = *params.MaxConcurrent
	}
	if len(params.Settings) > 0 {
		updates["settings"] = []byte(params.Settings)
	}
	if params.Status != "" {
		if params.Status != store.LinkStatusActive && params.Status != store.LinkStatusDisabled {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidLinkStatus)))
			return
		}
		updates["status"] = params.Status
	}

	if len(updates) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgNoUpdatesProvided)))
		return
	}

	if err := m.linkStore.UpdateLink(ctx, linkID, updates); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "link", err.Error())))
		return
	}

	// Invalidate affected agents so AGENTS.md gets regenerated
	m.invalidateLinkAgents(ctx, linkID)
	m.emitTeamCacheInvalidate()
	emitAudit(m.eventBus, client, "agent_link.updated", "agent_link", linkID.String())

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Emit agent_link.updated event
	if m.msgBus != nil {
		updatedLink, linkErr := m.linkStore.GetLink(ctx, linkID)
		if linkErr == nil && updatedLink != nil {
			changes := make([]string, 0, len(updates))
			for k := range updates {
				changes = append(changes, k)
			}
			m.msgBus.Broadcast(bus.Event{
				Name: protocol.EventAgentLinkUpdated,
				Payload: protocol.AgentLinkUpdatedPayload{
					LinkID:         linkID.String(),
					SourceAgentKey: updatedLink.SourceAgentKey,
					TargetAgentKey: updatedLink.TargetAgentKey,
					Direction:      updatedLink.Direction,
					Status:         updatedLink.Status,
					Changes:        changes,
				},
			})
		}
	}
}

// --- Delete ---

type linksDeleteParams struct {
	LinkID string `json:"linkId"`
}

func (m *AgentLinksMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.linkStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgLinksNotConfigured)))
		return
	}

	var params linksDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.LinkID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "linkId")))
		return
	}

	linkID, err := uuid.Parse(params.LinkID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "linkId")))
		return
	}

	// Fetch link before deleting to get agent IDs for cache invalidation
	link, _ := m.linkStore.GetLink(ctx, linkID)

	if err := m.linkStore.DeleteLink(ctx, linkID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "link", err.Error())))
		return
	}

	// Invalidate affected agents so AGENTS.md gets regenerated
	if link != nil {
		m.invalidateLinkAgentsByID(ctx, link.SourceAgentID, link.TargetAgentID)
	}
	m.emitTeamCacheInvalidate()
	emitAudit(m.eventBus, client, "agent_link.deleted", "agent_link", linkID.String())

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Emit agent_link.deleted event
	if m.msgBus != nil && link != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventAgentLinkDeleted,
			Payload: protocol.AgentLinkDeletedPayload{
				LinkID:         linkID.String(),
				SourceAgentKey: link.SourceAgentKey,
				TargetAgentKey: link.TargetAgentKey,
			},
		})
	}
}

// emitTeamCacheInvalidate broadcasts a cache invalidation event for team data.
// Called when links change since team-member links affect team resolution.
func (m *AgentLinksMethods) emitTeamCacheInvalidate() {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindTeam},
	})
}

// invalidateLinkAgents fetches a link by ID and invalidates both source and target agent caches.
func (m *AgentLinksMethods) invalidateLinkAgents(ctx context.Context, linkID uuid.UUID) {
	if m.agentRouter == nil {
		return
	}
	link, err := m.linkStore.GetLink(ctx, linkID)
	if err != nil || link == nil {
		return
	}
	m.invalidateLinkAgentsByID(ctx, link.SourceAgentID, link.TargetAgentID)
}

// invalidateLinkAgentsByID invalidates agent caches by looking up agent keys from UUIDs.
func (m *AgentLinksMethods) invalidateLinkAgentsByID(ctx context.Context, sourceID, targetID uuid.UUID) {
	if m.agentRouter == nil {
		return
	}
	if src, err := m.agentStore.GetByID(ctx, sourceID); err == nil {
		m.agentRouter.InvalidateAgent(src.AgentKey)
	}
	if tgt, err := m.agentStore.GetByID(ctx, targetID); err == nil {
		m.agentRouter.InvalidateAgent(tgt.AgentKey)
	}
}

// --- helpers ---

func resolveAgentUUID(agentStore store.AgentStore, keyOrID string) (uuid.UUID, error) {
	if id, err := uuid.Parse(keyOrID); err == nil {
		ag, err := agentStore.GetByID(context.Background(), id)
		if err != nil {
			return uuid.Nil, err
		}
		return ag.ID, nil
	}
	ag, err := agentStore.GetByKey(context.Background(), keyOrID)
	if err != nil {
		return uuid.Nil, err
	}
	return ag.ID, nil
}

// resolveAgentInfo returns full agent data for validation and cache invalidation.
func resolveAgentInfo(agentStore store.AgentStore, keyOrID string) (*store.AgentData, error) {
	if id, err := uuid.Parse(keyOrID); err == nil {
		return agentStore.GetByID(context.Background(), id)
	}
	return agentStore.GetByKey(context.Background(), keyOrID)
}
