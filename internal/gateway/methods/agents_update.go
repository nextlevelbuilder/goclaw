package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
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
		AgentID           string `json:"agentId"`
		Name              string `json:"name"`
		Workspace         string `json:"workspace"`
		Provider          string `json:"provider"`
		Model             string `json:"model"`
		Avatar            string `json:"avatar"`
		Status            string `json:"status"`
		Frontmatter       string `json:"frontmatter"`
		ContextWindow     *int   `json:"context_window"`
		MaxToolIterations *int   `json:"max_tool_iterations"`
		IsDefault         *bool  `json:"is_default"`
		BudgetCents       *int   `json:"budget_monthly_cents"`
		// Per-agent config overrides
		ToolsConfig      json.RawMessage `json:"tools_config,omitempty"`
		SubagentsConfig  json.RawMessage `json:"subagents_config,omitempty"`
		SandboxConfig    json.RawMessage `json:"sandbox_config,omitempty"`
		MemoryConfig     json.RawMessage `json:"memory_config,omitempty"`
		CompactionConfig json.RawMessage `json:"compaction_config,omitempty"`
		ContextPruning   json.RawMessage `json:"context_pruning,omitempty"`
		OtherConfig      json.RawMessage `json:"other_config,omitempty"`
		// Promoted config fields
		Emoji               *string         `json:"emoji,omitempty"`
		AgentDescription    *string         `json:"agent_description,omitempty"`
		ThinkingLevel       *string         `json:"thinking_level,omitempty"`
		MaxTokens           *int            `json:"max_tokens,omitempty"`
		SelfEvolve          *bool           `json:"self_evolve,omitempty"`
		SkillEvolve         *bool           `json:"skill_evolve,omitempty"`
		SkillNudgeInterval  *int            `json:"skill_nudge_interval,omitempty"`
		ReasoningConfig     json.RawMessage `json:"reasoning_config,omitempty"`
		WorkspaceSharing    json.RawMessage `json:"workspace_sharing,omitempty"`
		ChatGPTOAuthRouting json.RawMessage `json:"chatgpt_oauth_routing,omitempty"`
		ShellDenyGroups     json.RawMessage `json:"shell_deny_groups,omitempty"`
		KGDedupConfig       json.RawMessage `json:"kg_dedup_config,omitempty"`
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
		if params.Provider != "" {
			updates["provider"] = params.Provider
		}
		if params.Model != "" {
			updates["model"] = params.Model
		}
		if params.Status != "" {
			updates["status"] = params.Status
		}
		if params.Frontmatter != "" {
			updates["frontmatter"] = params.Frontmatter
		}
		if params.ContextWindow != nil {
			updates["context_window"] = *params.ContextWindow
		}
		if params.MaxToolIterations != nil {
			updates["max_tool_iterations"] = *params.MaxToolIterations
		}
		if params.IsDefault != nil {
			updates["is_default"] = *params.IsDefault
		}
		if params.BudgetCents != nil {
			updates["budget_monthly_cents"] = *params.BudgetCents
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
			// Validate v3 flag values (must be boolean) before persisting.
			var otherMap map[string]any
			if json.Unmarshal(params.OtherConfig, &otherMap) == nil {
				if err := store.ValidateV3Flags(otherMap); err != nil {
					client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
					return
				}
			}
			updates["other_config"] = []byte(params.OtherConfig)
		}
		// Promoted config fields
		if params.Emoji != nil {
			updates["emoji"] = *params.Emoji
		}
		if params.AgentDescription != nil {
			updates["agent_description"] = *params.AgentDescription
		}
		if params.ThinkingLevel != nil {
			updates["thinking_level"] = *params.ThinkingLevel
		}
		if params.MaxTokens != nil {
			updates["max_tokens"] = *params.MaxTokens
		}
		if params.SelfEvolve != nil {
			updates["self_evolve"] = *params.SelfEvolve
		}
		if params.SkillEvolve != nil {
			updates["skill_evolve"] = *params.SkillEvolve
		}
		if params.SkillNudgeInterval != nil {
			v := max(*params.SkillNudgeInterval,
				// DB column is NOT NULL DEFAULT 0
				0)
			updates["skill_nudge_interval"] = v
		}
		if len(params.ReasoningConfig) > 0 {
			updates["reasoning_config"] = []byte(params.ReasoningConfig)
		}
		if len(params.WorkspaceSharing) > 0 {
			updates["workspace_sharing"] = []byte(params.WorkspaceSharing)
		}
		if len(params.ChatGPTOAuthRouting) > 0 {
			updates["chatgpt_oauth_routing"] = []byte(params.ChatGPTOAuthRouting)
		}
		if len(params.ShellDenyGroups) > 0 {
			updates["shell_deny_groups"] = []byte(params.ShellDenyGroups)
		}
		if len(params.KGDedupConfig) > 0 {
			updates["kg_dedup_config"] = []byte(params.KGDedupConfig)
		}

		if len(updates) > 0 {
			if err := m.agentStore.Update(ctx, ag.ID, updates); err != nil {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "agent", fmt.Sprintf("%v", err))))
				return
			}
		}

		// Update identity in DB bootstrap — targeted field replacement to preserve all other fields.
		if params.Avatar != "" || params.Name != "" {
			// Read existing agent-level IDENTITY.md content.
			existingContent := ""
			if dbFiles, err := m.agentStore.GetAgentContextFiles(ctx, ag.ID); err == nil {
				for _, f := range dbFiles {
					if f.FileName == "IDENTITY.md" {
						existingContent = f.Content
						break
					}
				}
			}

			// Apply targeted replacements, preserving all other fields (Creature, Purpose, Vibe, etc.).
			newContent := existingContent
			if params.Name != "" {
				newContent = bootstrap.UpdateIdentityField(newContent, "Name", params.Name)
			}
			if params.Avatar != "" {
				newContent = bootstrap.UpdateIdentityField(newContent, "Avatar", params.Avatar)
			}
			// Fallback: if content was empty (no IDENTITY.md yet), build minimal content.
			if strings.TrimSpace(newContent) == "" {
				newContent = buildIdentityContent(params.Name, "", params.Avatar)
			}

			if err := m.agentStore.SetAgentContextFile(ctx, ag.ID, "IDENTITY.md", newContent); err != nil {
				slog.Warn("failed to update agent IDENTITY.md", "agent", params.AgentID, "error", err)
			}

			// For open agents: also update Name in all per-user IDENTITY.md copies.
			// Per-user files take precedence in LoadContextFiles, so they must be updated too.
			if params.Name != "" && ag.AgentType == store.AgentTypeOpen {
				if userFiles, err := m.agentStore.ListUserContextFilesByName(ctx, ag.ID, "IDENTITY.md"); err == nil {
					for _, uf := range userFiles {
						updated := bootstrap.UpdateIdentityField(uf.Content, "Name", params.Name)
						if updated == uf.Content {
							continue // no change needed
						}
						if err := m.agentStore.SetUserContextFile(ctx, ag.ID, uf.UserID, "IDENTITY.md", updated); err != nil {
							slog.Warn("failed to update user IDENTITY.md on rename", "agent", params.AgentID, "user", uf.UserID, "error", err)
						}
					}
				}
			}

			// Invalidate interceptor cache so updated IDENTITY.md is served immediately.
			if m.interceptor != nil {
				m.interceptor.InvalidateAgent(ag.ID)
			}
		}

		// Post-Phase-2 canonicalization: router cache entries are always keyed
		// as `tenantID:agent_key`. The previous belt-and-suspenders UUID-based
		// invalidation was dead code — exact-segment match never matches a
		// UUID as the final segment of a canonical cache key.
		m.agents.InvalidateAgent(params.AgentID)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"agentId": params.AgentID,
	}))
	emitAudit(m.eventBus, client, "agent.updated", "agent", params.AgentID)
}
