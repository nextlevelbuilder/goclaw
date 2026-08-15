package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerConfigPermissionCRUDTools registers the goclaw_config_permissions_*
// MCP tools backed by store.ConfigPermissionStore.
func registerConfigPermissionCRUDTools(srv *mcpserver.MCPServer, perms store.ConfigPermissionStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_config_permissions_list",
		mcpgo.WithDescription("List config permissions for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("config_type", mcpgo.Description("Config type to filter by (\"file_writer\", \"heartbeat\", \"cron\", \"context_files\", or \"*\").")),
		mcpgo.WithString("scope", mcpgo.Description("Scope to filter by; empty lists all scopes.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleConfigPermissionsList(perms))

	srv.AddTool(mcpgo.NewTool("goclaw_config_permissions_check",
		mcpgo.WithDescription("Check a config permission decision for an agent/scope/user."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("scope", mcpgo.Required(), mcpgo.Description("Permission scope.")),
		mcpgo.WithString("config_type", mcpgo.Required(), mcpgo.Description("Config type.")),
		mcpgo.WithString("user_id", mcpgo.Required(), mcpgo.Description("User ID to check.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleConfigPermissionsCheck(perms))

	srv.AddTool(mcpgo.NewTool("goclaw_config_permissions_grant",
		mcpgo.WithDescription("Grant a config permission."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("scope", mcpgo.Required(), mcpgo.Description("Permission scope.")),
		mcpgo.WithString("config_type", mcpgo.Required(), mcpgo.Description("Config type.")),
		mcpgo.WithString("user_id", mcpgo.Required(), mcpgo.Description("User ID to grant to.")),
		mcpgo.WithString("permission", mcpgo.Required(), mcpgo.Enum("allow", "deny"), mcpgo.Description("\"allow\" or \"deny\".")),
		mcpgo.WithString("granted_by", mcpgo.Description("User ID recorded as the granter.")),
	), handleConfigPermissionsGrant(perms))

	srv.AddTool(mcpgo.NewTool("goclaw_config_permissions_revoke",
		mcpgo.WithDescription("Revoke a config permission."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("scope", mcpgo.Required(), mcpgo.Description("Permission scope.")),
		mcpgo.WithString("config_type", mcpgo.Required(), mcpgo.Description("Config type.")),
		mcpgo.WithString("user_id", mcpgo.Required(), mcpgo.Description("User ID to revoke from.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleConfigPermissionsRevoke(perms))
}

func handleConfigPermissionsList(perms store.ConfigPermissionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("config_permissions.list", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("config_permissions.list", fmt.Errorf("invalid agent_id: %w", err))
		}
		configType := req.GetString("config_type", "")
		scope := req.GetString("scope", "")
		list, err := perms.List(ctx, agentID, configType, scope)
		if err != nil {
			return toolError("config_permissions.list", err)
		}
		return jsonToolResult(map[string]any{"permissions": list})
	}
}

func handleConfigPermissionsCheck(perms store.ConfigPermissionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("config_permissions.check", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("config_permissions.check", fmt.Errorf("invalid agent_id: %w", err))
		}
		scope, err := req.RequireString("scope")
		if err != nil {
			return toolError("config_permissions.check", err)
		}
		configType, err := req.RequireString("config_type")
		if err != nil {
			return toolError("config_permissions.check", err)
		}
		userID, err := req.RequireString("user_id")
		if err != nil {
			return toolError("config_permissions.check", err)
		}
		decision, err := store.CheckConfigPermissionDecision(ctx, perms, agentID, scope, configType, userID)
		if err != nil {
			return toolError("config_permissions.check", err)
		}
		return jsonToolResult(map[string]any{"decision": decision})
	}
}

func handleConfigPermissionsGrant(perms store.ConfigPermissionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("config_permissions.grant", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("config_permissions.grant", fmt.Errorf("invalid agent_id: %w", err))
		}
		scope, err := req.RequireString("scope")
		if err != nil {
			return toolError("config_permissions.grant", err)
		}
		configType, err := req.RequireString("config_type")
		if err != nil {
			return toolError("config_permissions.grant", err)
		}
		userID, err := req.RequireString("user_id")
		if err != nil {
			return toolError("config_permissions.grant", err)
		}
		permission, err := req.RequireString("permission")
		if err != nil {
			return toolError("config_permissions.grant", err)
		}
		if !store.ValidConfigPermission(permission) {
			return mcpgo.NewToolResultError("config_permissions.grant: permission must be \"allow\" or \"deny\""), nil
		}

		perm := &store.ConfigPermission{
			ID:         store.GenNewID(),
			AgentID:    agentID,
			Scope:      scope,
			ConfigType: configType,
			UserID:     userID,
			Permission: permission,
		}
		if grantedBy := req.GetString("granted_by", ""); grantedBy != "" {
			perm.GrantedBy = &grantedBy
		}
		if err := perms.Grant(ctx, perm); err != nil {
			return toolError("config_permissions.grant", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleConfigPermissionsRevoke(perms store.ConfigPermissionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("config_permissions.revoke", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("config_permissions.revoke", fmt.Errorf("invalid agent_id: %w", err))
		}
		scope, err := req.RequireString("scope")
		if err != nil {
			return toolError("config_permissions.revoke", err)
		}
		configType, err := req.RequireString("config_type")
		if err != nil {
			return toolError("config_permissions.revoke", err)
		}
		userID, err := req.RequireString("user_id")
		if err != nil {
			return toolError("config_permissions.revoke", err)
		}
		if err := perms.Revoke(ctx, agentID, scope, configType, userID); err != nil {
			return toolError("config_permissions.revoke", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}
