package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// AgentsMethods handles agents.list, agents.create, agents.update, agents.delete,
// agents.files.list/get/set, agent.identity.get.
type AgentsMethods struct {
	agents      *agent.Router
	cfg         *config.Config
	cfgPath     string
	workspace   string
	agentStore  store.AgentStore
	interceptor *tools.ContextFileInterceptor // invalidated on file writes
	eventBus    bus.EventPublisher
}

func NewAgentsMethods(agents *agent.Router, cfg *config.Config, cfgPath, workspace string, agentStore store.AgentStore, interceptor *tools.ContextFileInterceptor, eventBus bus.EventPublisher) *AgentsMethods {
	return &AgentsMethods{agents: agents, cfg: cfg, cfgPath: cfgPath, workspace: workspace, agentStore: agentStore, interceptor: interceptor, eventBus: eventBus}
}

// isOwnerUser checks if the given user ID is in the configured owner IDs.
func (m *AgentsMethods) isOwnerUser(userID string) bool {
	return canSeeAll(permissions.RoleViewer, m.cfg.Gateway.OwnerIDs, userID)
}

func (m *AgentsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodAgent, m.handleAgent)
	router.Register(protocol.MethodAgentWait, m.handleAgentWait)
	router.Register(protocol.MethodAgentsList, m.handleList)
	router.Register(protocol.MethodAgentsCreate, m.handleCreate)
	router.Register(protocol.MethodAgentsUpdate, m.handleUpdate)
	router.Register(protocol.MethodAgentsDelete, m.handleDelete)
	router.Register(protocol.MethodAgentsFileList, m.handleFilesList)
	router.Register(protocol.MethodAgentsFileGet, m.handleFilesGet)
	router.Register(protocol.MethodAgentsFileSet, m.handleFilesSet)
	router.Register(protocol.MethodAgentIdentityGet, m.handleIdentityGet)
}

type agentParams struct {
	AgentID string `json:"agentId"`
}

func (m *AgentsMethods) handleAgent(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params agentParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}

	loop, err := m.agents.Get(ctx, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"id":        loop.ID(),
		"isRunning": loop.IsRunning(),
	}))
}

func (m *AgentsMethods) handleAgentWait(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params agentParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}

	loop, err := m.agents.Get(ctx, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	// Return current status (blocking wait is a future enhancement).
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"id":     loop.ID(),
		"status": "idle",
	}))
}

func (m *AgentsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.agentStore != nil {
		locale := store.LocaleFromContext(ctx)
		userID := client.UserID()
		if userID == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserCtxRequired)))
			return
		}

		var agents []store.AgentData
		var err error
		if m.isOwnerUser(userID) {
			agents, err = m.agentStore.List(ctx, "")
		} else {
			agents, err = m.agentStore.ListAccessible(ctx, userID)
		}
		if err != nil {
			slog.Warn("agents.list: store query failed", "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "agents")))
			return
		}

		infos := make([]map[string]any, 0, len(agents))
		for _, a := range agents {
			if a.Status != store.AgentStatusActive {
				continue
			}
			// Keep legacy keys (id/name) for backwards compat with any
			// existing client; also expose the underlying DB row UUID +
			// the picker-friendly fields (agent_key, display_name, emoji,
			// agent_description, is_default, max_tool_iterations) so the
			// website can render the Agents picker without a per-agent
			// GET round-trip. Without is_default + emoji here, the website
			// can't find the Default row or draw the icon grid.
			infos = append(infos, map[string]any{
				"id":                  a.AgentKey, // legacy: agent_key as id
				"name":                a.DisplayName,
				"agent_id":            a.ID.String(), // explicit DB row UUID
				"agent_key":           a.AgentKey,
				"display_name":        a.DisplayName,
				"emoji":               a.Emoji,
				"agent_description":   a.AgentDescription,
				"is_default":          a.IsDefault,
				"max_tool_iterations": a.MaxToolIterations,
				"context_window":      a.ContextWindow,
				"model":               a.Model,
				"provider":            a.Provider,
				"agentType":           a.AgentType,
				"agent_type":          a.AgentType,
				"status":              a.Status,
				"isRunning":           m.agents.IsRunning(ctx, a.AgentKey),
				// Agent's custom instructions (migration 000063). The website's
				// Manage modal's Clone button pre-fills the create form from
				// this field — must round-trip or clones lose the prompt.
				"system_prompt": a.SystemPrompt,
			})
		}
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"agents": infos,
		}))
		return
	}

	// Fallback: return router-cached agents.
	infos := m.agents.ListInfo()
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"agents": infos,
	}))
}
