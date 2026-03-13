package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
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
	if userID == "" {
		return false
	}
	return slices.Contains(m.cfg.Gateway.OwnerIDs, userID)
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

func (m *AgentsMethods) handleAgent(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params agentParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}

	loop, err := m.agents.Get(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"id":        loop.ID(),
		"isRunning": loop.IsRunning(),
	}))
}

func (m *AgentsMethods) handleAgentWait(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params agentParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}

	loop, err := m.agents.Get(params.AgentID)
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
			infos = append(infos, map[string]any{
				"id":        a.AgentKey,
				"name":      a.DisplayName,
				"model":     a.Model,
				"provider":  a.Provider,
				"agentType": a.AgentType,
				"status":    a.Status,
				"isRunning": m.agents.IsRunning(a.AgentKey),
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
