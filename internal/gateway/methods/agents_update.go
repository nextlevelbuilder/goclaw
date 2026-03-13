package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- agents.update ---
// Matching TS src/gateway/server-methods/agents.ts:288-346

func (m *AgentsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}

	if m.agentStore != nil {
		// --- DB-backed: update agent in store ---
		ctx := context.Background()
		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
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
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "agent", fmt.Sprintf("%v", err))))
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
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
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
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "config", err.Error())))
			return
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"agentId": params.AgentID,
	}))
	emitAudit(m.eventBus, client, "agent.updated", "agent", params.AgentID)
}
