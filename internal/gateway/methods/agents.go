package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
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
}

func NewAgentsMethods(agents *agent.Router, cfg *config.Config, cfgPath, workspace string, agentStore store.AgentStore, interceptor *tools.ContextFileInterceptor) *AgentsMethods {
	return &AgentsMethods{agents: agents, cfg: cfg, cfgPath: cfgPath, workspace: workspace, agentStore: agentStore, interceptor: interceptor}
}

// isOwnerUser checks if the given user ID is in the configured owner IDs.
func (m *AgentsMethods) isOwnerUser(userID string) bool {
	if userID == "" {
		return false
	}
	for _, id := range m.cfg.Gateway.OwnerIDs {
		if id == userID {
			return true
		}
	}
	return false
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

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
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
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"id":     loop.ID(),
		"status": "idle",
	}))
}

func (m *AgentsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.agentStore != nil {
		userID := client.UserID()
		if userID == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "user context required"))
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
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to list agents"))
			return
		}

		infos := make([]map[string]interface{}, 0, len(agents))
		for _, a := range agents {
			if a.Status != store.AgentStatusActive {
				continue
			}
			infos = append(infos, map[string]interface{}{
				"id":        a.AgentKey,
				"name":      a.DisplayName,
				"model":     a.Model,
				"provider":  a.Provider,
				"agentType": a.AgentType,
				"status":    a.Status,
				"isRunning": m.agents.IsRunning(a.AgentKey),
			})
		}
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
			"agents": infos,
		}))
		return
	}

	// Fallback: return router-cached agents.
	infos := m.agents.ListInfo()
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"agents": infos,
	}))
}

// --- agents.create ---
// Matching TS src/gateway/server-methods/agents.ts:216-287

func (m *AgentsMethods) handleCreate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Name      string   `json:"name"`
		Workspace string   `json:"workspace"`
		Emoji     string   `json:"emoji"`
		Avatar    string   `json:"avatar"`
		AgentType string   `json:"agent_type"`              // "open" (default) or "predefined"
		OwnerIDs  []string `json:"owner_ids,omitempty"`     // first entry used as DB owner_id; falls back to "system"
		// Per-agent config overrides
		ToolsConfig      json.RawMessage `json:"tools_config,omitempty"`
		SubagentsConfig  json.RawMessage `json:"subagents_config,omitempty"`
		SandboxConfig    json.RawMessage `json:"sandbox_config,omitempty"`
		MemoryConfig     json.RawMessage `json:"memory_config,omitempty"`
		CompactionConfig json.RawMessage `json:"compaction_config,omitempty"`
		ContextPruning   json.RawMessage `json:"context_pruning,omitempty"`
		OtherConfig      json.RawMessage `json:"other_config,omitempty"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "name is required"))
		return
	}

	agentType := params.AgentType
	if agentType == "" {
		agentType = store.AgentTypeOpen
	}

	agentID := config.NormalizeAgentID(params.Name)
	if agentID == "default" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "cannot create agent with reserved id 'default'"))
		return
	}

	// Resolve workspace
	ws := params.Workspace
	if ws == "" {
		ws = filepath.Join(m.workspace, "agents", agentID)
	} else {
		ws = config.ExpandHome(ws)
	}

	if m.agentStore != nil {
		// --- Managed mode: create agent in DB ---
		ctx := context.Background()

		// Check if agent already exists in DB
		if existing, _ := m.agentStore.GetByKey(ctx, agentID); existing != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "agent already exists: "+agentID))
			return
		}

		// Resolve owner: use first provided ID so external provisioning tools (e.g. goclaw-wizards)
		// can set a real user as owner at creation time. Falls back to "system" for backward compat.
		ownerID := "system"
		if len(params.OwnerIDs) > 0 && params.OwnerIDs[0] != "" {
			ownerID = params.OwnerIDs[0]
		}

		agentData := &store.AgentData{
			AgentKey:         agentID,
			DisplayName:      params.Name,
			OwnerID:          ownerID,
			AgentType:        agentType,
			Provider:         m.cfg.Agents.Defaults.Provider,
			Model:            m.cfg.Agents.Defaults.Model,
			Workspace:        ws,
			Status:           store.AgentStatusActive,
			ToolsConfig:      params.ToolsConfig,
			SubagentsConfig:  params.SubagentsConfig,
			SandboxConfig:    params.SandboxConfig,
			MemoryConfig:     params.MemoryConfig,
			CompactionConfig: params.CompactionConfig,
			ContextPruning:   params.ContextPruning,
			OtherConfig:      params.OtherConfig,
		}
		if err := m.agentStore.Create(ctx, agentData); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, fmt.Sprintf("failed to create agent: %v", err)))
			return
		}

		// Seed context files to DB (skipped for open agents)
		if _, err := bootstrap.SeedToStore(ctx, m.agentStore, agentData.ID, agentData.AgentType); err != nil {
			slog.Warn("failed to seed bootstrap for agent", "agent", agentID, "error", err)
		}

		// Set identity in DB bootstrap
		if params.Name != "" || params.Emoji != "" || params.Avatar != "" {
			content := buildIdentityContent(params.Name, params.Emoji, params.Avatar)
			if err := m.agentStore.SetAgentContextFile(ctx, agentData.ID, "IDENTITY.md", content); err != nil {
				slog.Warn("failed to set IDENTITY.md", "agent", agentID, "error", err)
			}
		}

		// Invalidate router cache so resolver re-loads from DB
		m.agents.InvalidateAgent(agentID)
	} else {
		// --- Fallback: config.json + filesystem ---
		if _, ok := m.cfg.Agents.List[agentID]; ok {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "agent already exists: "+agentID))
			return
		}

		spec := config.AgentSpec{
			DisplayName: params.Name,
			Workspace:   ws,
		}
		if params.Emoji != "" || params.Avatar != "" {
			spec.Identity = &config.IdentityConfig{
				Emoji: params.Emoji,
			}
		}

		if m.cfg.Agents.List == nil {
			m.cfg.Agents.List = make(map[string]config.AgentSpec)
		}
		m.cfg.Agents.List[agentID] = spec

		if err := config.Save(m.cfgPath, m.cfg); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to save config: "+err.Error()))
			return
		}

		// Append identity metadata to IDENTITY.md
		if params.Name != "" || params.Emoji != "" || params.Avatar != "" {
			identityPath := filepath.Join(ws, "IDENTITY.md")
			appendIdentityFields(identityPath, params.Name, params.Emoji, params.Avatar)
		}
	}

	// Both modes: create workspace dir + seed filesystem backup
	os.MkdirAll(ws, 0755)
	bootstrap.EnsureWorkspaceFiles(ws)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"ok":        true,
		"agentId":   agentID,
		"name":      params.Name,
		"workspace": ws,
	}))
}

// --- agents.update ---
// Matching TS src/gateway/server-methods/agents.ts:288-346

func (m *AgentsMethods) handleUpdate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		AgentID   string `json:"agentId"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Model     string `json:"model"`
		Avatar    string `json:"avatar"`
		// Per-agent config overrides
		ToolsConfig      json.RawMessage `json:"tools_config,omitempty"`
		SubagentsConfig  json.RawMessage `json:"subagents_config,omitempty"`
		SandboxConfig    json.RawMessage `json:"sandbox_config,omitempty"`
		MemoryConfig     json.RawMessage `json:"memory_config,omitempty"`
		CompactionConfig json.RawMessage `json:"compaction_config,omitempty"`
		ContextPruning   json.RawMessage `json:"context_pruning,omitempty"`
		OtherConfig      json.RawMessage `json:"other_config,omitempty"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "agentId is required"))
		return
	}

	if m.agentStore != nil {
		// --- Managed mode: update agent in DB ---
		ctx := context.Background()
		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "agent not found: "+params.AgentID))
			return
		}

		updates := map[string]any{}
		if params.Name != "" {
			updates["display_name"] = params.Name
		}
		if params.Workspace != "" {
			ws := config.ExpandHome(params.Workspace)
			updates["workspace"] = ws
			os.MkdirAll(ws, 0755)
		}
		if params.Model != "" {
			updates["model"] = params.Model
		}
		// Per-agent JSONB config overrides
		if len(params.ToolsConfig) > 0 {
			updates["tools_config"] = []byte(params.ToolsConfig)
		}
		if len(params.SubagentsConfig) > 0 {
			updates["subagents_config"] = []byte(params.SubagentsConfig)
		}
		if len(params.SandboxConfig) > 0 {
			updates["sandbox_config"] = []byte(params.SandboxConfig)
		}
		if len(params.MemoryConfig) > 0 {
			updates["memory_config"] = []byte(params.MemoryConfig)
		}
		if len(params.CompactionConfig) > 0 {
			updates["compaction_config"] = []byte(params.CompactionConfig)
		}
		if len(params.ContextPruning) > 0 {
			updates["context_pruning"] = []byte(params.ContextPruning)
		}
		if len(params.OtherConfig) > 0 {
			updates["other_config"] = []byte(params.OtherConfig)
		}

		if len(updates) > 0 {
			if err := m.agentStore.Update(ctx, ag.ID, updates); err != nil {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, fmt.Sprintf("failed to update agent: %v", err)))
				return
			}
		}

		// Update identity in DB bootstrap
		if params.Avatar != "" || params.Name != "" {
			content := buildIdentityContent(params.Name, "", params.Avatar)
			if err := m.agentStore.SetAgentContextFile(ctx, ag.ID, "IDENTITY.md", content); err != nil {
				slog.Warn("failed to update IDENTITY.md", "agent", params.AgentID, "error", err)
			}
			// Invalidate interceptor cache so updated IDENTITY.md is served immediately
			if m.interceptor != nil {
				m.interceptor.InvalidateAgent(ag.ID)
			}
		}

		m.agents.InvalidateAgent(params.AgentID)
	} else {
		// --- Fallback: config.json ---
		spec, ok := m.cfg.Agents.List[params.AgentID]
		if !ok {
			if params.AgentID != "default" {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "agent not found: "+params.AgentID))
				return
			}
		}

		if params.Name != "" {
			spec.DisplayName = params.Name
		}
		if params.Workspace != "" {
			spec.Workspace = config.ExpandHome(params.Workspace)
			os.MkdirAll(spec.Workspace, 0755)
		}
		if params.Model != "" {
			spec.Model = params.Model
		}

		if params.AgentID == "default" {
			if params.Model != "" {
				m.cfg.Agents.Defaults.Model = params.Model
			}
			if params.Workspace != "" {
				m.cfg.Agents.Defaults.Workspace = params.Workspace
			}
		} else {
			m.cfg.Agents.List[params.AgentID] = spec
		}

		if params.Avatar != "" {
			ws := spec.Workspace
			if ws == "" {
				ws = config.ExpandHome(m.cfg.Agents.Defaults.Workspace)
			}
			identityPath := filepath.Join(ws, "IDENTITY.md")
			appendIdentityFields(identityPath, "", "", params.Avatar)
		}

		if err := config.Save(m.cfgPath, m.cfg); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to save config: "+err.Error()))
			return
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"ok":      true,
		"agentId": params.AgentID,
	}))
}

// --- agents.delete ---
// Matching TS src/gateway/server-methods/agents.ts:347-398

func (m *AgentsMethods) handleDelete(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		AgentID     string `json:"agentId"`
		DeleteFiles bool   `json:"deleteFiles"`
	}
	params.DeleteFiles = true // default
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "agentId is required"))
		return
	}
	if params.AgentID == "default" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "cannot delete the default agent"))
		return
	}

	var removedBindings int

	if m.agentStore != nil {
		// --- Managed mode: delete from DB ---
		ctx := context.Background()
		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "agent not found: "+params.AgentID))
			return
		}

		if err := m.agentStore.Delete(ctx, ag.ID); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, fmt.Sprintf("failed to delete agent: %v", err)))
			return
		}

		m.agents.InvalidateAgent(params.AgentID)
		m.agents.Remove(params.AgentID)

		// Best-effort delete workspace
		if params.DeleteFiles && ag.Workspace != "" {
			os.RemoveAll(ag.Workspace)
		}
	} else {
		// --- Fallback: config.json ---
		spec, ok := m.cfg.Agents.List[params.AgentID]
		if !ok {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "agent not found: "+params.AgentID))
			return
		}

		delete(m.cfg.Agents.List, params.AgentID)

		var kept []config.AgentBinding
		for _, b := range m.cfg.Bindings {
			if b.AgentID == params.AgentID {
				removedBindings++
			} else {
				kept = append(kept, b)
			}
		}
		m.cfg.Bindings = kept

		m.agents.Remove(params.AgentID)

		if err := config.Save(m.cfgPath, m.cfg); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "failed to save config: "+err.Error()))
			return
		}

		if params.DeleteFiles && spec.Workspace != "" {
			ws := config.ExpandHome(spec.Workspace)
			os.RemoveAll(ws)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"ok":              true,
		"agentId":         params.AgentID,
		"removedBindings": removedBindings,
	}))
}
