package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// doImportNewAgent creates a new agent from the archive, returning import summary.
func (h *AgentsHandler) doImportNewAgent(ctx context.Context, r *http.Request, arc *importArchive, progressFn func(ProgressEvent)) (*ImportSummary, error) {
	tenantID := store.TenantIDFromContext(ctx)
	userID := store.UserIDFromContext(ctx)

	// Build agent record from archive config + optional overrides
	agentKey := r.FormValue("agent_key")
	displayName := r.FormValue("display_name")

	if agentKey == "" && arc.agentConfig["agent_key"] != nil {
		json.Unmarshal(arc.agentConfig["agent_key"], &agentKey) //nolint:errcheck
	}
	if displayName == "" && arc.agentConfig["display_name"] != nil {
		json.Unmarshal(arc.agentConfig["display_name"], &displayName) //nolint:errcheck
	}
	if agentKey == "" && displayName != "" {
		agentKey = config.NormalizeAgentID(displayName)
	}
	if agentKey == "" {
		return nil, errors.New("agent_key is required (not found in archive or request)")
	}

	// Dedup: suffix with -N if key already exists
	agentKey = h.dedupAgentKey(ctx, agentKey)

	ag := h.buildAgentFromArchive(arc.agentConfig, agentKey, displayName, tenantID, userID)

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "config", Status: "running"})
	}

	if err := h.agents.Create(ctx, ag); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "config", Status: "done", Current: 1, Total: 1})
	}

	sections := map[string]bool{
		"context_files":   true,
		"memory":          true,
		"knowledge_graph": true,
		"cron":            true,
		"user_profiles":   true,
		"user_overrides":  true,
		"workspace":       true,
		"team":            true,
		"episodic":        true,
		"evolution":       true,
		"vault":           true,
	}
	summary, err := h.doMergeImport(ctx, ag, arc, sections, progressFn)
	if err != nil {
		// Best-effort: agent already created, log but return partial summary
		slog.Error("agents.import.merge_data", "agent_id", ag.ID, "error", err)
		return &ImportSummary{AgentID: ag.ID.String(), AgentKey: ag.AgentKey}, err
	}
	summary.AgentID = ag.ID.String()
	summary.AgentKey = ag.AgentKey
	return summary, nil
}

// buildAgentFromArchive constructs an AgentData from the parsed archive config map.
func (h *AgentsHandler) buildAgentFromArchive(cfg map[string]json.RawMessage, agentKey, displayName string, tenantID uuid.UUID, ownerID string) *store.AgentData {
	ag := &store.AgentData{
		AgentKey:    agentKey,
		DisplayName: displayName,
		TenantID:    tenantID,
		OwnerID:     ownerID,
		Status:      store.AgentStatusActive,
	}
	unmarshalField(cfg, "frontmatter", &ag.Frontmatter)
	unmarshalField(cfg, "provider", &ag.Provider)
	unmarshalField(cfg, "model", &ag.Model)
	unmarshalField(cfg, "agent_type", &ag.AgentType)
	unmarshalField(cfg, "context_window", &ag.ContextWindow)
	unmarshalField(cfg, "max_tool_iterations", &ag.MaxToolIterations)

	ag.ToolsConfig = rawOrNil(cfg["tools_config"])
	ag.SandboxConfig = rawOrNil(cfg["sandbox_config"])
	ag.SubagentsConfig = rawOrNil(cfg["subagents_config"])
	ag.MemoryConfig = rawOrNil(cfg["memory_config"])
	ag.CompactionConfig = rawOrNil(cfg["compaction_config"])
	ag.ContextPruning = rawOrNil(cfg["context_pruning"])
	ag.OtherConfig = rawOrNil(cfg["other_config"])

	// Promoted config fields — try top-level first, fall back to other_config for legacy exports
	unmarshalField(cfg, "emoji", &ag.Emoji)
	unmarshalField(cfg, "agent_description", &ag.AgentDescription)
	unmarshalField(cfg, "thinking_level", &ag.ThinkingLevel)
	unmarshalField(cfg, "max_tokens", &ag.MaxTokens)
	unmarshalField(cfg, "self_evolve", &ag.SelfEvolve)
	unmarshalField(cfg, "skill_evolve", &ag.SkillEvolve)
	unmarshalField(cfg, "skill_nudge_interval", &ag.SkillNudgeInterval)
	ag.ReasoningConfig = rawOrNil(cfg["reasoning_config"])
	ag.WorkspaceSharing = rawOrNil(cfg["workspace_sharing"])
	ag.ChatGPTOAuthRouting = rawOrNil(cfg["chatgpt_oauth_routing"])
	ag.ShellDenyGroups = rawOrNil(cfg["shell_deny_groups"])
	ag.KGDedupConfig = rawOrNil(cfg["kg_dedup_config"])

	// Backward compat: extract promoted fields from other_config for pre-migration exports
	if len(ag.OtherConfig) > 2 {
		var oc map[string]json.RawMessage
		if json.Unmarshal(ag.OtherConfig, &oc) == nil {
			extractLegacy := func(key string, dest *string) {
				if *dest == "" {
					if v, ok := oc[key]; ok {
						var s string
						if json.Unmarshal(v, &s) == nil {
							*dest = s
						}
					}
				}
			}
			extractLegacy("emoji", &ag.Emoji)
			extractLegacy("description", &ag.AgentDescription)
			extractLegacy("thinking_level", &ag.ThinkingLevel)
			if ag.ReasoningConfig == nil {
				ag.ReasoningConfig = rawOrNil(oc["reasoning"])
			}
			if ag.WorkspaceSharing == nil {
				ag.WorkspaceSharing = rawOrNil(oc["workspace_sharing"])
			}
			if ag.ChatGPTOAuthRouting == nil {
				ag.ChatGPTOAuthRouting = rawOrNil(oc["chatgpt_oauth_routing"])
			}
			if ag.ShellDenyGroups == nil {
				ag.ShellDenyGroups = rawOrNil(oc["shell_deny_groups"])
			}
			if ag.KGDedupConfig == nil {
				ag.KGDedupConfig = rawOrNil(oc["kg_dedup_config"])
			}
		}
	}

	if ag.AgentType == "" {
		ag.AgentType = store.AgentTypeOpen
	}
	if ag.ContextWindow <= 0 {
		ag.ContextWindow = config.DefaultContextWindow
	}
	if ag.MaxToolIterations <= 0 {
		ag.MaxToolIterations = config.DefaultMaxIterations
	}
	ag.Workspace = fmt.Sprintf("%s/%s", h.defaultWorkspace, ag.AgentKey)
	ag.RestrictToWorkspace = true

	if len(ag.CompactionConfig) == 0 {
		ag.CompactionConfig = json.RawMessage(`{}`)
	}
	if len(ag.MemoryConfig) == 0 {
		ag.MemoryConfig = json.RawMessage(`{"enabled":true}`)
	}
	return ag
}

// dedupAgentKey appends -2, -3, ... until the key is unique in the store.
func (h *AgentsHandler) dedupAgentKey(ctx context.Context, base string) string {
	key := base
	for i := 2; i <= 100; i++ {
		if existing, _ := h.agents.GetByKey(ctx, key); existing == nil {
			return key
		}
		key = fmt.Sprintf("%s-%d", base, i)
	}
	return key
}
