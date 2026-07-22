package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AgentRuntimeLookup resolves a live agent's running state without the mcp
// package depending on internal/agent directly — internal/agent already
// imports internal/mcp (loop_mcp_user.go), so importing agent.Router here
// would create an import cycle. Callers (internal/gateway.Server) close over
// their *agent.Router to satisfy this.
type AgentRuntimeLookup func(ctx context.Context, agentID string) (id string, isRunning bool, err error)

// allowedAgentContextFiles mirrors internal/gateway/methods/agents_files.go's
// allowedAgentFiles list (TOOLS.md intentionally excluded, not applicable via
// this surface). Duplicated here rather than imported since crud_*.go is a
// standalone MCP surface that does not depend on internal/gateway/methods.
var allowedAgentContextFiles = []string{
	bootstrap.AgentsFile, bootstrap.SoulFile, bootstrap.IdentityFile,
	bootstrap.UserFile, bootstrap.UserPredefinedFile, bootstrap.CapabilitiesFile,
	bootstrap.BootstrapFile, bootstrap.MemoryJSONFile,
	bootstrap.HeartbeatFile,
}

// registerAgentCRUDTools registers the goclaw_agents_* MCP tools backed by store.AgentStore.
func registerAgentCRUDTools(srv *mcpserver.MCPServer, agents store.AgentStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_agents_list",
		mcpgo.WithDescription("List goclaw agents, optionally scoped to a specific owner."),
		mcpgo.WithString("owner_id", mcpgo.Description("Filter by owner ID; empty lists all agents visible to the caller.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentsList(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_get",
		mcpgo.WithDescription("Get a single goclaw agent by UUID or agent_key."),
		mcpgo.WithString("id", mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("agent_key", mcpgo.Description("Agent key/slug, used when id is not known.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentsGet(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_create",
		mcpgo.WithDescription("Create a new goclaw agent."),
		mcpgo.WithString("agent_key", mcpgo.Required(), mcpgo.Description("Unique agent key/slug (e.g. \"support-bot\").")),
		mcpgo.WithString("display_name", mcpgo.Description("Human-readable agent name; defaults to agent_key.")),
		mcpgo.WithString("owner_id", mcpgo.Description("Owner user ID; defaults to \"system\".")),
		mcpgo.WithString("provider", mcpgo.Description("LLM provider name (e.g. \"anthropic\").")),
		mcpgo.WithString("model", mcpgo.Description("LLM model name.")),
		mcpgo.WithString("workspace", mcpgo.Description("Workspace directory path for this agent.")),
		mcpgo.WithString("agent_type", mcpgo.Description("\"open\" or \"predefined\"; defaults to \"predefined\".")),
	), handleAgentsCreate(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_update",
		mcpgo.WithDescription("Apply a partial update to an existing agent."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("display_name", mcpgo.Description("New display name.")),
		mcpgo.WithString("provider", mcpgo.Description("New LLM provider.")),
		mcpgo.WithString("model", mcpgo.Description("New LLM model.")),
		mcpgo.WithString("status", mcpgo.Description("New agent status.")),
		mcpgo.WithNumber("context_window", mcpgo.Description("New context window size in tokens.")),
		mcpgo.WithNumber("max_tool_iterations", mcpgo.Description("New max tool iterations per turn.")),
	), handleAgentsUpdate(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_delete",
		mcpgo.WithDescription("Delete a goclaw agent by UUID."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleAgentsDelete(agents))
}

func handleAgentsList(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		ownerID := req.GetString("owner_id", "")
		list, err := agents.List(ctx, ownerID)
		if err != nil {
			return toolError("agents.list", err)
		}
		return jsonToolResult(list)
	}
}

func handleAgentsGet(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr := req.GetString("id", "")
		agentKey := req.GetString("agent_key", "")
		switch {
		case idStr != "":
			id, err := uuid.Parse(idStr)
			if err != nil {
				return toolError("agents.get", fmt.Errorf("invalid id: %w", err))
			}
			agent, err := agents.GetByID(ctx, id)
			if err != nil {
				return toolError("agents.get", err)
			}
			return jsonToolResult(agent)
		case agentKey != "":
			agent, err := agents.GetByKey(ctx, agentKey)
			if err != nil {
				return toolError("agents.get", err)
			}
			return jsonToolResult(agent)
		default:
			return mcpgo.NewToolResultError("agents.get: one of id or agent_key is required"), nil
		}
	}
}

func handleAgentsCreate(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentKey, err := req.RequireString("agent_key")
		if err != nil {
			return toolError("agents.create", err)
		}

		ownerID := req.GetString("owner_id", "system")
		agentType := req.GetString("agent_type", store.AgentTypePredefined)
		displayName := req.GetString("display_name", agentKey)

		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			tenantID = store.MasterTenantID
		}

		data := &store.AgentData{
			BaseModel:   store.BaseModel{ID: store.GenNewID()},
			TenantID:    tenantID,
			AgentKey:    agentKey,
			DisplayName: displayName,
			OwnerID:     ownerID,
			Provider:    req.GetString("provider", ""),
			Model:       req.GetString("model", ""),
			Workspace:   req.GetString("workspace", ""),
			AgentType:   agentType,
			Status:      "active",
		}

		if err := agents.Create(ctx, data); err != nil {
			return toolError("agents.create", err)
		}
		return jsonToolResult(data)
	}
}

func handleAgentsUpdate(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("agents.update", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("agents.update", fmt.Errorf("invalid id: %w", err))
		}

		updates := map[string]any{}
		args := req.GetArguments()
		for _, key := range []string{"display_name", "provider", "model", "status"} {
			if v, ok := args[key]; ok {
				updates[key] = v
			}
		}
		if v, ok := args["context_window"]; ok {
			updates["context_window"] = v
		}
		if v, ok := args["max_tool_iterations"]; ok {
			updates["max_tool_iterations"] = v
		}
		if len(updates) == 0 {
			return mcpgo.NewToolResultError("agents.update: no fields to update"), nil
		}

		if err := agents.Update(ctx, id, updates); err != nil {
			return toolError("agents.update", err)
		}
		agent, err := agents.GetByID(ctx, id)
		if err != nil {
			return toolError("agents.update", err)
		}
		return jsonToolResult(agent)
	}
}

func handleAgentsDelete(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("agents.delete", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("agents.delete", fmt.Errorf("invalid id: %w", err))
		}
		if err := agents.Delete(ctx, id); err != nil {
			return toolError("agents.delete", err)
		}
		return jsonToolResult(map[string]bool{"deleted": true})
	}
}

// registerAgentRuntimeCRUDTools registers the goclaw_agent_{get,wait,identity_get}
// and goclaw_agents_files_{list,get,set} MCP tools. goclaw_agent_{get,wait}
// need the live agent runtime (for running-state) in addition to
// store.AgentStore (for context files/identity), unlike the plain CRUD tools
// above.
func registerAgentRuntimeCRUDTools(srv *mcpserver.MCPServer, agents store.AgentStore, lookup AgentRuntimeLookup) {
	srv.AddTool(mcpgo.NewTool("goclaw_agent_get",
		mcpgo.WithDescription("Return the running state for a single goclaw agent."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key/slug (or \"default\").")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentRuntimeGet(lookup))

	srv.AddTool(mcpgo.NewTool("goclaw_agent_wait",
		mcpgo.WithDescription("Wait for (or report the current status of) a goclaw agent."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key/slug (or \"default\").")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentWait(lookup))

	srv.AddTool(mcpgo.NewTool("goclaw_agent_identity_get",
		mcpgo.WithDescription("Return identity metadata (name, emoji, avatar, description) for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Description("Agent key/slug.")),
		mcpgo.WithString("session_key", mcpgo.Description("Session key to extract the agent ID from, when agent_id is not known.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentIdentityGet(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_files_list",
		mcpgo.WithDescription("List the well-known context files for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Description("Agent key/slug (or \"default\").")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentsFilesList(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_files_get",
		mcpgo.WithDescription("Read a single well-known context file for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Description("Agent key/slug (or \"default\").")),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("File name (e.g. \"SOUL.md\").")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentsFilesGet(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_files_set",
		mcpgo.WithDescription("Write a single well-known context file for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Description("Agent key/slug (or \"default\").")),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("File name (e.g. \"SOUL.md\").")),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("New file content.")),
		mcpgo.WithBoolean("propagate", mcpgo.Description("Also push this change to all existing per-user instances of the file (default false).")),
	), handleAgentsFilesSet(agents))
}

func isAllowedAgentContextFile(name string) bool {
	return slices.Contains(allowedAgentContextFiles, name)
}

func handleAgentRuntimeGet(lookup AgentRuntimeLookup) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID := req.GetString("agent_id", "default")
		id, isRunning, err := lookup(ctx, agentID)
		if err != nil {
			return toolError("agent.get", err)
		}
		return jsonToolResult(map[string]any{"id": id, "isRunning": isRunning})
	}
}

func handleAgentWait(lookup AgentRuntimeLookup) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID := req.GetString("agent_id", "default")
		id, isRunning, err := lookup(ctx, agentID)
		if err != nil {
			return toolError("agent.wait", err)
		}
		status := "idle"
		if isRunning {
			status = "running"
		}
		return jsonToolResult(map[string]any{"id": id, "status": status})
	}
}

// parseIdentityContent parses IDENTITY.md content string and extracts Key: Value fields.
// Mirrors internal/gateway/methods/agents_identity.go's unexported helper of
// the same name — duplicated for the same reason as allowedAgentContextFiles.
func parseIdentityContent(content string) map[string]string {
	result := make(map[string]string)
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if val != "" {
				result[key] = val
			}
		}
	}
	return result
}

func handleAgentIdentityGet(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID := req.GetString("agent_id", "")
		if agentID == "" {
			if sessionKey := req.GetString("session_key", ""); sessionKey != "" {
				parts := strings.SplitN(sessionKey, ":", 3)
				if len(parts) >= 2 {
					agentID = parts[1]
				}
			}
			if agentID == "" {
				agentID = "default"
			}
		}

		result := map[string]any{"agentId": agentID}
		ag, err := agents.GetByKey(ctx, agentID)
		if err != nil {
			return jsonToolResult(result)
		}
		result["name"] = ag.DisplayName

		dbFiles, _ := agents.GetAgentContextFiles(ctx, ag.ID)
		for _, f := range dbFiles {
			if f.FileName != bootstrap.IdentityFile {
				continue
			}
			identity := parseIdentityContent(f.Content)
			if identity["Name"] != "" {
				result["name"] = identity["Name"]
			}
			if identity["Emoji"] != "" {
				result["emoji"] = identity["Emoji"]
			}
			if identity["Avatar"] != "" {
				result["avatar"] = identity["Avatar"]
			}
			if identity["Description"] != "" {
				result["description"] = identity["Description"]
			}
			break
		}
		return jsonToolResult(result)
	}
}

func handleAgentsFilesList(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID := req.GetString("agent_id", "default")
		ag, err := agents.GetByKey(ctx, agentID)
		if err != nil {
			return toolError("agents.files.list", err)
		}
		dbFiles, err := agents.GetAgentContextFiles(ctx, ag.ID)
		if err != nil {
			return toolError("agents.files.list", err)
		}
		dbMap := make(map[string]store.AgentContextFileData, len(dbFiles))
		for _, f := range dbFiles {
			dbMap[f.FileName] = f
		}
		files := make([]map[string]any, 0, len(allowedAgentContextFiles))
		for _, name := range allowedAgentContextFiles {
			if f, ok := dbMap[name]; ok {
				files = append(files, map[string]any{"name": name, "missing": false, "size": len(f.Content)})
			} else {
				files = append(files, map[string]any{"name": name, "missing": true})
			}
		}
		return jsonToolResult(map[string]any{"agentId": agentID, "files": files})
	}
}

func handleAgentsFilesGet(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID := req.GetString("agent_id", "default")
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("agents.files.get", err)
		}
		if !isAllowedAgentContextFile(name) {
			return mcpgo.NewToolResultError("agents.files.get: file not allowed: " + name), nil
		}
		ag, err := agents.GetByKey(ctx, agentID)
		if err != nil {
			return toolError("agents.files.get", err)
		}
		dbFiles, err := agents.GetAgentContextFiles(ctx, ag.ID)
		if err != nil {
			return toolError("agents.files.get", err)
		}
		for _, f := range dbFiles {
			if f.FileName == name {
				return jsonToolResult(map[string]any{
					"agentId": agentID,
					"file":    map[string]any{"name": name, "missing": false, "size": len(f.Content), "content": f.Content},
				})
			}
		}
		return jsonToolResult(map[string]any{
			"agentId": agentID,
			"file":    map[string]any{"name": name, "missing": true},
		})
	}
}

func handleAgentsFilesSet(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID := req.GetString("agent_id", "default")
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("agents.files.set", err)
		}
		if !isAllowedAgentContextFile(name) {
			return mcpgo.NewToolResultError("agents.files.set: file not allowed: " + name), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return toolError("agents.files.set", err)
		}
		ag, err := agents.GetByKey(ctx, agentID)
		if err != nil {
			return toolError("agents.files.set", err)
		}
		if err := agents.SetAgentContextFile(ctx, ag.ID, name, content); err != nil {
			return toolError("agents.files.set", err)
		}
		propagated := 0
		if req.GetBool("propagate", false) {
			n, err := agents.PropagateContextFile(ctx, ag.ID, name)
			if err == nil {
				propagated = n
			}
		}
		return jsonToolResult(map[string]any{
			"agentId":    agentID,
			"file":       map[string]any{"name": name, "missing": false, "size": len(content), "content": content},
			"propagated": propagated,
		})
	}
}

// maxPinnedSkillsPerAgent mirrors the invariant documented on
// store.AgentData.ParsePinnedSkills (agent_store.go) — pinned skills are
// always-loaded into every turn's system prompt, so the count is capped to
// bound prompt size.
const maxPinnedSkillsPerAgent = 10

// registerAgentSkillPinCRUDTools registers goclaw_agents_pin_skill/
// goclaw_agents_unpin_skill. Pinning is distinct from granting access
// (registerSkillGrantCRUDTools in crud_skills.go): a pinned skill is
// auto-loaded into the agent's system prompt every turn (see
// internal/agent/resolver.go's ParsePinnedSkills / PinnedSkillsSummary),
// while a grant only makes the skill available for on-demand use_skill
// calls. There is no dedicated pin/unpin RPC on the gateway — the web UI
// sets other_config.pinned_skills via the general agents.update WS method
// (internal/gateway/methods/agents_update.go), which replaces the whole
// other_config JSONB blob wholesale. These handlers replicate that by
// reading the agent's current other_config, splicing pinned_skills, and
// writing the full blob back — a naive partial write would silently drop
// every other other_config field (self_evolution_metrics, tts_params, etc).
func registerAgentSkillPinCRUDTools(srv *mcpserver.MCPServer, agents store.AgentStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_agents_pin_skill",
		mcpgo.WithDescription("Pin a skill onto an agent so it's auto-loaded into that agent's system prompt every turn (distinct from granting access)."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("skill", mcpgo.Required(), mcpgo.Description("Skill slug/name to pin.")),
	), handleAgentsPinSkill(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_unpin_skill",
		mcpgo.WithDescription("Unpin a skill from an agent (does not revoke access, only removes it from the always-loaded set)."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("skill", mcpgo.Required(), mcpgo.Description("Skill slug/name to unpin.")),
	), handleAgentsUnpinSkill(agents))
}

// spliceOtherConfigPinnedSkills reads ag's other_config JSONB, applies edit
// to its pinned_skills list, and returns the full re-marshaled blob ready
// to pass as agents.Update's "other_config" value (a full-blob replace, not
// a merge — see registerAgentSkillPinCRUDTools doc comment).
func spliceOtherConfigPinnedSkills(ag store.AgentData, edit func(current []string) ([]string, error)) (json.RawMessage, error) {
	bag := map[string]any{}
	if len(ag.OtherConfig) > 0 {
		if err := json.Unmarshal(ag.OtherConfig, &bag); err != nil {
			return nil, fmt.Errorf("cannot parse existing other_config: %w", err)
		}
	}
	next, err := edit(ag.ParsePinnedSkills())
	if err != nil {
		return nil, err
	}
	bag["pinned_skills"] = next
	return json.Marshal(bag)
}

func handleAgentsPinSkill(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("agents.pin_skill", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("agents.pin_skill", fmt.Errorf("invalid id: %w", err))
		}
		skill, err := req.RequireString("skill")
		if err != nil {
			return toolError("agents.pin_skill", err)
		}

		ag, err := agents.GetByID(ctx, id)
		if err != nil {
			return toolError("agents.pin_skill", err)
		}

		raw, err := spliceOtherConfigPinnedSkills(*ag, func(current []string) ([]string, error) {
			if slices.Contains(current, skill) {
				return current, nil
			}
			if len(current) >= maxPinnedSkillsPerAgent {
				return nil, fmt.Errorf("agent already has %d pinned skills (max %d)", len(current), maxPinnedSkillsPerAgent)
			}
			return append(current, skill), nil
		})
		if err != nil {
			return toolError("agents.pin_skill", err)
		}

		if err := agents.Update(ctx, id, map[string]any{"other_config": []byte(raw)}); err != nil {
			return toolError("agents.pin_skill", err)
		}
		updated, err := agents.GetByID(ctx, id)
		if err != nil {
			return toolError("agents.pin_skill", err)
		}
		return jsonToolResult(map[string]any{"ok": "true", "pinned_skills": updated.ParsePinnedSkills()})
	}
}

func handleAgentsUnpinSkill(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("agents.unpin_skill", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("agents.unpin_skill", fmt.Errorf("invalid id: %w", err))
		}
		skill, err := req.RequireString("skill")
		if err != nil {
			return toolError("agents.unpin_skill", err)
		}

		ag, err := agents.GetByID(ctx, id)
		if err != nil {
			return toolError("agents.unpin_skill", err)
		}

		raw, err := spliceOtherConfigPinnedSkills(*ag, func(current []string) ([]string, error) {
			out := make([]string, 0, len(current))
			for _, s := range current {
				if s != skill {
					out = append(out, s)
				}
			}
			return out, nil
		})
		if err != nil {
			return toolError("agents.unpin_skill", err)
		}

		if err := agents.Update(ctx, id, map[string]any{"other_config": []byte(raw)}); err != nil {
			return toolError("agents.unpin_skill", err)
		}
		updated, err := agents.GetByID(ctx, id)
		if err != nil {
			return toolError("agents.unpin_skill", err)
		}
		return jsonToolResult(map[string]any{"ok": "true", "pinned_skills": updated.ParsePinnedSkills()})
	}
}
