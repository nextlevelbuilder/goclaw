package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- agents.create ---
// Matching TS src/gateway/server-methods/agents.ts:216-287

func (m *AgentsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Name              string   `json:"name"`
		Workspace         string   `json:"workspace"`
		Emoji             string   `json:"emoji"`
		Avatar            string   `json:"avatar"`
		Provider          string   `json:"provider"`
		Model             string   `json:"model"`
		AgentType         string   `json:"agent_type"`          // "open" (default) or "predefined"
		OwnerIDs          []string `json:"owner_ids,omitempty"` // first entry used as DB owner_id; falls back to "system"
		TenantID          string   `json:"tenant_id"`           // required for cross-tenant callers; ignored otherwise
		ContextWindow     int      `json:"context_window"`
		MaxToolIterations int      `json:"max_tool_iterations"`
		BudgetCents       *int     `json:"budget_monthly_cents"`
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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}

	agentType := params.AgentType
	if agentType == "" {
		agentType = store.AgentTypeOpen
	}

	agentID := config.NormalizeAgentID(params.Name)
	if agentID == "default" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "cannot create agent with reserved id 'default'")))
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
		// --- DB-backed: create agent in store ---

		// Check if agent already exists in DB
		if existing, _ := m.agentStore.GetByKey(ctx, agentID); existing != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAlreadyExists, "agent", agentID)))
			return
		}

		// Resolve owner: use first provided ID so external provisioning tools (e.g. goclaw-wizards)
		// can set a real user as owner at creation time. Falls back to "system" for backward compat.
		ownerID := "system"
		if len(params.OwnerIDs) > 0 && params.OwnerIDs[0] != "" {
			ownerID = params.OwnerIDs[0]
		}

		// Resolve tenant_id: explicit param for cross-tenant; otherwise inherit from connection scope.
		var tenantID uuid.UUID
		if client.IsOwner() {
			if params.TenantID != "" {
				tid, err := uuid.Parse(params.TenantID)
				if err != nil {
					client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
					return
				}
				tenantID = tid
			} else {
				tenantID = client.TenantID()
			}
		} else {
			tenantID = client.TenantID()
		}

		provider := params.Provider
		if provider == "" {
			provider = m.cfg.Agents.Defaults.Provider
		}
		model := params.Model
		if model == "" {
			model = m.cfg.Agents.Defaults.Model
		}

		agentData := &store.AgentData{
			AgentKey:         agentID,
			DisplayName:      params.Name,
			OwnerID:          ownerID,
			TenantID:         tenantID,
			AgentType:        agentType,
			Provider:         provider,
			Model:            model,
			Workspace:        ws,
			ContextWindow:     params.ContextWindow,
			MaxToolIterations: params.MaxToolIterations,
			BudgetMonthlyCents: params.BudgetCents,
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
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "agent", fmt.Sprintf("%v", err))))
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
	}

	// Both modes: create workspace dir + seed filesystem backup
	os.MkdirAll(ws, 0755)
	bootstrap.EnsureWorkspaceFiles(ws)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":        true,
		"agentId":   agentID,
		"name":      params.Name,
		"workspace": ws,
	}))
	emitAudit(m.eventBus, client, "agent.created", "agent", agentID)
}
