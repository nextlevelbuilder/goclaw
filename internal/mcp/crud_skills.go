package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

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

		var skillID uuid.UUID
		if idStr != "" {
			id, err := uuid.Parse(idStr)
			if err != nil {
				return toolError("skills.update", fmt.Errorf("invalid id: %w", err))
			}
			skillID = id
		} else {
			info, ok := skills.GetSkill(ctx, name)
			if !ok {
				return mcpgo.NewToolResultError("skills.update: skill not found: " + name), nil
			}
			id, err := uuid.Parse(info.ID)
			if err != nil {
				return toolError("skills.update", fmt.Errorf("cannot resolve skill id: %w", err))
			}
			skillID = id
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
