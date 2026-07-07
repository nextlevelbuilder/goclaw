package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerSkillCRUDTools registers the goclaw_skills_* MCP tools backed by store.SkillStore.
func registerSkillCRUDTools(srv *mcpserver.MCPServer, skills store.SkillStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_skills_list",
		mcpgo.WithDescription("List all skills known to goclaw."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSkillsList(skills))

	srv.AddTool(mcpgo.NewTool("goclaw_skills_get",
		mcpgo.WithDescription("Get metadata for a single skill by name."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Skill name.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSkillsGet(skills))
}

func handleSkillsList(skills store.SkillStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list := skills.ListSkills(ctx)
		return jsonToolResult(list)
	}
}

func handleSkillsGet(skills store.SkillStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("skills.get", err)
		}
		skill, ok := skills.GetSkill(ctx, name)
		if !ok {
			return mcpgo.NewToolResultError("skills.get: skill not found: " + name), nil
		}
		return jsonToolResult(skill)
	}
}

// registerSkillUpdateCRUDTool registers goclaw_skills_update. Only wired when
// the skill store also implements store.SkillManageStore (e.g. PGSkillStore);
// stores that don't support updates (e.g. FileSkillStore) simply don't get
// this tool registered.
func registerSkillUpdateCRUDTool(srv *mcpserver.MCPServer, skills store.SkillStore, manage store.SkillManageStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_skills_update",
		mcpgo.WithDescription("Update a goclaw skill's metadata by name or id, applying the given field updates."),
		mcpgo.WithString("name", mcpgo.Description("Skill name; used to resolve the skill if id is not given.")),
		mcpgo.WithString("id", mcpgo.Description("Skill UUID.")),
		mcpgo.WithObject("updates", mcpgo.Required(), mcpgo.Description("Field updates to apply (e.g. {\"visibility\": \"tenant\"}).")),
	), handleSkillsUpdate(skills, manage))
}

func handleSkillsUpdate(skills store.SkillStore, manage store.SkillManageStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name := req.GetString("name", "")
		idStr := req.GetString("id", "")
		if name == "" && idStr == "" {
			return mcpgo.NewToolResultError("skills.update: one of name or id is required"), nil
		}

		skillID, err := resolveSkillID(ctx, skills, idStr, name)
		if err != nil {
			return toolError("skills.update", err)
		}

		args := req.GetArguments()
		rawUpdates, ok := args["updates"].(map[string]any)
		if !ok || len(rawUpdates) == 0 {
			return mcpgo.NewToolResultError("skills.update: updates is required"), nil
		}

		if err := manage.UpdateSkill(ctx, skillID, rawUpdates); err != nil {
			return toolError("skills.update", err)
		}
		skills.BumpVersion()
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

// registerSkillCreateCRUDTool registers goclaw_skills_create, letting MCP
// callers create a new managed skill from SKILL.md content — the single-file
// equivalent of the web UI's ZIP-based skill upload
// (SkillsHandler.handleUpload in internal/http/skills_upload.go), via
// skills.CreateFromContent. There is no per-caller identity on this MCP
// surface (see crud_server.go doc comment), so owner_id is an explicit
// param — same pattern as registerAgentCRUDTools' goclaw_agents_create,
// which defaults owner_id to "system" when omitted.
func registerSkillCreateCRUDTool(srv *mcpserver.MCPServer, manage store.SkillManageStore, cfg *config.Config) {
	srv.AddTool(mcpgo.NewTool("goclaw_skills_create",
		mcpgo.WithDescription("Create a new managed skill from a SKILL.md content string (name/slug/description are parsed from its YAML frontmatter)."),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("Full SKILL.md content, including YAML frontmatter (name, slug, description).")),
		mcpgo.WithString("owner_id", mcpgo.Description("Owner user ID; defaults to \"system\".")),
	), handleSkillsCreate(manage, cfg))
}

func handleSkillsCreate(manage store.SkillManageStore, cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		content, err := req.RequireString("content")
		if err != nil {
			return toolError("skills.create", err)
		}
		ownerID := req.GetString("owner_id", "system")

		tenantID := store.TenantIDFromContext(ctx)
		tenantSlug := store.TenantSlugFromContext(ctx)
		tenantSkillsDir := config.TenantSkillsStoreDir(cfg.DataDir, tenantID, tenantSlug)

		id, slug, err := skills.CreateFromContent(ctx, manage, tenantSkillsDir, content, ownerID)
		if err != nil {
			switch {
			case errors.Is(err, skills.ErrSkillNameRequired):
				return mcpgo.NewToolResultError("skills.create: name is required in SKILL.md frontmatter"), nil
			case errors.Is(err, skills.ErrSkillSlugInvalid):
				return mcpgo.NewToolResultError("skills.create: invalid slug"), nil
			case errors.Is(err, skills.ErrSkillSlugConflict):
				return mcpgo.NewToolResultError("skills.create: slug conflicts with a system skill"), nil
			case errors.Is(err, skills.ErrSkillGuardRejected):
				return mcpgo.NewToolResultError("skills.create: " + err.Error()), nil
			default:
				return toolError("skills.create", err)
			}
		}
		return jsonToolResult(map[string]any{"id": id.String(), "slug": slug})
	}
}

// registerSkillWriteFileCRUDTool registers goclaw_skills_write_file, letting
// MCP callers edit a managed (non-system) skill's file content on disk —
// mirroring the web UI's skill file editor (SkillsHandler.handleWriteFile in
// internal/http/skills_versions.go). Both surfaces call the same
// skills.WriteVersionedFile helper so validation and versioning stay
// identical. Only wired when the skill store implements
// store.SkillManageStore, same gate as registerSkillUpdateCRUDTool.
func registerSkillWriteFileCRUDTool(srv *mcpserver.MCPServer, skillStore store.SkillStore, manage store.SkillManageStore, cfg *config.Config) {
	srv.AddTool(mcpgo.NewTool("goclaw_skills_write_file",
		mcpgo.WithDescription("Write a file's content within a managed (non-system) skill, creating a new immutable version of that skill."),
		mcpgo.WithString("name", mcpgo.Description("Skill name; used to resolve the skill if id is not given.")),
		mcpgo.WithString("id", mcpgo.Description("Skill UUID.")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("File path relative to the skill's directory (e.g. \"SKILL.md\").")),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("New full content of the file.")),
	), handleSkillsWriteFile(skillStore, manage, cfg))
}

func handleSkillsWriteFile(skillStore store.SkillStore, manage store.SkillManageStore, cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name := req.GetString("name", "")
		idStr := req.GetString("id", "")
		if name == "" && idStr == "" {
			return mcpgo.NewToolResultError("skills.write_file: one of name or id is required"), nil
		}

		relPath, err := req.RequireString("path")
		if err != nil {
			return toolError("skills.write_file", err)
		}
		content, err := req.RequireString("content")
		if err != nil {
			return toolError("skills.write_file", err)
		}

		skillID, err := resolveSkillID(ctx, skillStore, idStr, name)
		if err != nil {
			return toolError("skills.write_file", err)
		}

		tenantID := store.TenantIDFromContext(ctx)
		tenantSlug := store.TenantSlugFromContext(ctx)
		tenantSkillsDir := config.TenantSkillsStoreDir(cfg.DataDir, tenantID, tenantSlug)

		path, version, err := skills.WriteVersionedFile(ctx, manage, tenantSkillsDir, skillID, relPath, content)
		if err != nil {
			switch {
			case errors.Is(err, skills.ErrSkillFileNotFound):
				return mcpgo.NewToolResultError("skills.write_file: file or skill not found"), nil
			case errors.Is(err, skills.ErrSkillIsSystem):
				return mcpgo.NewToolResultError("skills.write_file: cannot edit a system skill"), nil
			case errors.Is(err, skills.ErrSkillInvalidPath):
				return mcpgo.NewToolResultError("skills.write_file: invalid file path"), nil
			default:
				return toolError("skills.write_file", err)
			}
		}
		return jsonToolResult(map[string]any{"ok": "true", "path": path, "version": version})
	}
}

// registerSkillGrantCRUDTools registers goclaw_skills_grant/goclaw_skills_revoke,
// letting MCP callers grant/revoke an agent's access to a skill — mirrors the
// goclaw CLI's `skills grant`/`skills revoke` (internal/http/skills_grants.go
// handleGrantAgent/handleRevokeAgent) via store.SkillManageStore.GrantToAgent/
// RevokeFromAgent. Note: granting access is distinct from pinning a skill
// into an agent's always-loaded context — see registerAgentSkillPinCRUDTools.
func registerSkillGrantCRUDTools(srv *mcpserver.MCPServer, skillStore store.SkillStore, manage store.SkillManageStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_skills_grant",
		mcpgo.WithDescription("Grant an agent access to a skill."),
		mcpgo.WithString("name", mcpgo.Description("Skill name; used to resolve the skill if id is not given.")),
		mcpgo.WithString("id", mcpgo.Description("Skill UUID.")),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID to grant access to.")),
		mcpgo.WithNumber("version", mcpgo.Description("Skill version to pin the grant to; defaults to the skill's current version.")),
		mcpgo.WithBoolean("can_manage", mcpgo.Description("Whether the granted agent can also edit/manage this skill (default false).")),
	), handleSkillsGrant(skillStore, manage))

	srv.AddTool(mcpgo.NewTool("goclaw_skills_revoke",
		mcpgo.WithDescription("Revoke an agent's access to a skill."),
		mcpgo.WithString("name", mcpgo.Description("Skill name; used to resolve the skill if id is not given.")),
		mcpgo.WithString("id", mcpgo.Description("Skill UUID.")),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID to revoke access from.")),
	), handleSkillsRevoke(skillStore, manage))
}

// resolveSkillID resolves a skill UUID from either an explicit id or a
// name lookup — shared by every goclaw_skills_* tool that accepts both.
func resolveSkillID(ctx context.Context, skillStore store.SkillStore, idStr, name string) (uuid.UUID, error) {
	if idStr != "" {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid id: %w", err)
		}
		return id, nil
	}
	info, ok := skillStore.GetSkill(ctx, name)
	if !ok {
		return uuid.Nil, fmt.Errorf("skill not found: %s", name)
	}
	id, err := uuid.Parse(info.ID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cannot resolve skill id: %w", err)
	}
	return id, nil
}

func handleSkillsGrant(skillStore store.SkillStore, manage store.SkillManageStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name := req.GetString("name", "")
		idStr := req.GetString("id", "")
		if name == "" && idStr == "" {
			return mcpgo.NewToolResultError("skills.grant: one of name or id is required"), nil
		}
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("skills.grant", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("skills.grant", fmt.Errorf("invalid agent_id: %w", err))
		}

		skillID, err := resolveSkillID(ctx, skillStore, idStr, name)
		if err != nil {
			return toolError("skills.grant", err)
		}

		version := 0
		if v, ok := req.GetArguments()["version"]; ok {
			if f, ok := v.(float64); ok {
				version = int(f)
			}
		}
		if version == 0 {
			info, ok := manage.GetSkillByID(ctx, skillID)
			if !ok {
				return mcpgo.NewToolResultError("skills.grant: skill not found"), nil
			}
			version = info.Version
		}

		canManage := req.GetBool("can_manage", false)
		if err := manage.GrantToAgent(ctx, skillID, agentID, version, "mcp", canManage); err != nil {
			return toolError("skills.grant", err)
		}
		skillStore.BumpVersion()
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleSkillsRevoke(skillStore store.SkillStore, manage store.SkillManageStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name := req.GetString("name", "")
		idStr := req.GetString("id", "")
		if name == "" && idStr == "" {
			return mcpgo.NewToolResultError("skills.revoke: one of name or id is required"), nil
		}
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("skills.revoke", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("skills.revoke", fmt.Errorf("invalid agent_id: %w", err))
		}

		skillID, err := resolveSkillID(ctx, skillStore, idStr, name)
		if err != nil {
			return toolError("skills.revoke", err)
		}

		if err := manage.RevokeFromAgent(ctx, skillID, agentID); err != nil {
			return toolError("skills.revoke", err)
		}
		skillStore.BumpVersion()
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
