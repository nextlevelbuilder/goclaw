package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerSystemConfigCRUDTools registers the goclaw_system_config_* MCP
// tools backed by store.SystemConfigStore — closes a CLI-vs-MCP coverage
// gap (`goclaw system-config list`, plus set/delete which the CLI reference
// didn't surface but the store supports).
func registerSystemConfigCRUDTools(srv *mcpserver.MCPServer, cfg store.SystemConfigStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_system_config_list",
		mcpgo.WithDescription("List all system config key/value pairs visible to the current tenant (master merged with tenant overrides)."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSystemConfigList(cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_system_config_get",
		mcpgo.WithDescription("Get a single system config value by key."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Config key.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSystemConfigGet(cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_system_config_set",
		mcpgo.WithDescription("Set a system config value for the current tenant."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Config key.")),
		mcpgo.WithString("value", mcpgo.Required(), mcpgo.Description("Config value.")),
	), handleSystemConfigSet(cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_system_config_delete",
		mcpgo.WithDescription("Delete a system config value for the current tenant."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Config key.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleSystemConfigDelete(cfg))
}

func handleSystemConfigList(cfg store.SystemConfigStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		all, err := cfg.List(ctx)
		if err != nil {
			return toolError("system_config.list", err)
		}
		return jsonToolResult(all)
	}
}

func handleSystemConfigGet(cfg store.SystemConfigStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("system_config.get", err)
		}
		value, err := cfg.Get(ctx, key)
		if err != nil {
			return toolError("system_config.get", err)
		}
		return jsonToolResult(map[string]string{"key": key, "value": value})
	}
}

func handleSystemConfigSet(cfg store.SystemConfigStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("system_config.set", err)
		}
		value, err := req.RequireString("value")
		if err != nil {
			return toolError("system_config.set", err)
		}
		if err := cfg.Set(ctx, key, value); err != nil {
			return toolError("system_config.set", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleSystemConfigDelete(cfg store.SystemConfigStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("system_config.delete", err)
		}
		if err := cfg.Delete(ctx, key); err != nil {
			return toolError("system_config.delete", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
