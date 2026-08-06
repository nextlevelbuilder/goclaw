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

		// ALWAYS ListAccessible. A personal agent is visible to its owner, to anyone
		// it was explicitly shared with, and to nobody else — including an org owner
		// or admin.
		//
		// There used to be a bypass here for cfg.Gateway.OwnerIDs, which returned
		// every agent in the tenant. As configured it matched nobody (staging sets
		// GOCLAW_OWNER_IDS=system, and no human user has the id "system"), so this
		// changes no behaviour today. It removes the LATENT hole: adding one real
		// user id to that env var would have silently granted a person read access
		// to every member's private agents, with no audit trail and nothing in the
		// UI to indicate it.
		//
		// Privacy should not depend on an environment variable being left alone.
		// Cross-tenant platform operations are unaffected — ListAccessible has its
		// own IsCrossTenant branch, which is an explicit scope rather than a
		// per-user exemption.
		agents, err := m.agentStore.ListAccessible(ctx, userID)
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
				// owner_id + is_locked: how a client tells a tenant BUILT-IN
				// (owner_id 'system', shared with every member, not editable) from a
				// personal agent. This map is hand-built, so unlike the HTTP handler —
				// which marshals the whole struct — a field missing here is simply
				// invisible over WS. The board's built-in styling silently never
				// applied for exactly that reason: it tested owner_id, which was not
				// being sent. ListAccessible uses this same predicate server-side, so
				// sending it keeps one definition of "shared" rather than two.
				"owner_id":            a.OwnerID,
				"is_locked":           a.IsLocked,
				// Same lesson as owner_id above, learned once already: a field
				// absent from this hand-built map is invisible to the client with
				// no error anywhere. The share-with-org toggle needs this to know
				// its OWN current state.
				"visibility":          a.Visibility,
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
				// Per-agent tool policy (ToolPolicySpec: profile/allow/deny/alsoAllow).
				// agents.update already ACCEPTS this field, so without it in the list
				// payload a client could only overwrite the policy blind — it had no
				// way to read what was already there. The canvas needs it to show which
				// capabilities an agent has been granted, and a read-modify-write of a
				// policy you cannot read is how allow lists get silently clobbered.
				"tools_config": a.ToolsConfig,
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
