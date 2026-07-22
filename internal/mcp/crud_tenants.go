package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerTenantsCRUDTools registers the goclaw_tenants_* MCP tools backed
// by store.TenantStore — closes a CLI-vs-MCP coverage gap (the `goclaw
// tenants create/list/users` commands had no MCP equivalent). deps.Tenants
// was already threaded through CRUDDeps for the "X-GoClaw-Tenant-Id" header
// resolution (see crud_server.go); this reuses the same store reference.
func registerTenantsCRUDTools(srv *mcpserver.MCPServer, tenants store.TenantStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_tenants_list",
		mcpgo.WithDescription("List all tenants."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTenantsList(tenants))

	srv.AddTool(mcpgo.NewTool("goclaw_tenants_get",
		mcpgo.WithDescription("Get a single tenant by UUID or slug."),
		mcpgo.WithString("id", mcpgo.Description("Tenant UUID.")),
		mcpgo.WithString("slug", mcpgo.Description("Tenant slug, used when id is not known.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTenantsGet(tenants))

	srv.AddTool(mcpgo.NewTool("goclaw_tenants_create",
		mcpgo.WithDescription("Create a new tenant."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Tenant display name.")),
		mcpgo.WithString("slug", mcpgo.Required(), mcpgo.Description("Tenant slug (URL/path-safe identifier).")),
	), handleTenantsCreate(tenants))

	srv.AddTool(mcpgo.NewTool("goclaw_tenants_users_list",
		mcpgo.WithDescription("List a tenant's member users."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Tenant UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTenantsUsersList(tenants))

	srv.AddTool(mcpgo.NewTool("goclaw_tenants_users_add",
		mcpgo.WithDescription("Add a user to a tenant with a given role."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Tenant UUID.")),
		mcpgo.WithString("user_id", mcpgo.Required(), mcpgo.Description("User ID to add.")),
		mcpgo.WithString("role", mcpgo.Description("Tenant role; defaults to \"member\".")),
	), handleTenantsUsersAdd(tenants))

	srv.AddTool(mcpgo.NewTool("goclaw_tenants_users_remove",
		mcpgo.WithDescription("Remove a user from a tenant."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Tenant UUID.")),
		mcpgo.WithString("user_id", mcpgo.Required(), mcpgo.Description("User ID to remove.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleTenantsUsersRemove(tenants))
}

func handleTenantsList(tenants store.TenantStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list, err := tenants.ListTenants(ctx)
		if err != nil {
			return toolError("tenants.list", err)
		}
		return jsonToolResult(list)
	}
}

func handleTenantsGet(tenants store.TenantStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr := req.GetString("id", "")
		slug := req.GetString("slug", "")
		switch {
		case idStr != "":
			id, err := uuid.Parse(idStr)
			if err != nil {
				return toolError("tenants.get", fmt.Errorf("invalid id: %w", err))
			}
			t, err := tenants.GetTenant(ctx, id)
			if err != nil {
				return toolError("tenants.get", err)
			}
			return jsonToolResult(t)
		case slug != "":
			t, err := tenants.GetTenantBySlug(ctx, slug)
			if err != nil {
				return toolError("tenants.get", err)
			}
			return jsonToolResult(t)
		default:
			return mcpgo.NewToolResultError("tenants.get: one of id or slug is required"), nil
		}
	}
}

func handleTenantsCreate(tenants store.TenantStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("tenants.create", err)
		}
		slug, err := req.RequireString("slug")
		if err != nil {
			return toolError("tenants.create", err)
		}
		t := &store.TenantData{
			ID:     store.GenNewID(),
			Name:   name,
			Slug:   slug,
			Status: "active",
		}
		if err := tenants.CreateTenant(ctx, t); err != nil {
			return toolError("tenants.create", err)
		}
		return jsonToolResult(t)
	}
}

func handleTenantsUsersList(tenants store.TenantStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("tenants.users_list", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("tenants.users_list", fmt.Errorf("invalid id: %w", err))
		}
		users, err := tenants.ListUsers(ctx, id)
		if err != nil {
			return toolError("tenants.users_list", err)
		}
		return jsonToolResult(users)
	}
}

func handleTenantsUsersAdd(tenants store.TenantStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("tenants.users_add", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("tenants.users_add", fmt.Errorf("invalid id: %w", err))
		}
		userID, err := req.RequireString("user_id")
		if err != nil {
			return toolError("tenants.users_add", err)
		}
		role := req.GetString("role", "member")
		if err := tenants.AddUser(ctx, id, userID, role); err != nil {
			return toolError("tenants.users_add", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleTenantsUsersRemove(tenants store.TenantStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("tenants.users_remove", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("tenants.users_remove", fmt.Errorf("invalid id: %w", err))
		}
		userID, err := req.RequireString("user_id")
		if err != nil {
			return toolError("tenants.users_remove", err)
		}
		if err := tenants.RemoveUser(ctx, id, userID); err != nil {
			return toolError("tenants.users_remove", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
