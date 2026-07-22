package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// registerHooksCRUDTools registers the goclaw_hooks_* MCP tools backed by
// hooks.HookStore. Mirrors internal/gateway/methods/hooks.go, minus the
// RBAC-role gate (this whole surface is gated by the CRUD MCP bearer token —
// see internal/mcp/crud_server.go doc comment) and minus hooks.test's dry-run
// support (no TestRunner is wired for this standalone MCP surface; skipped —
// see final report).
func registerHooksCRUDTools(srv *mcpserver.MCPServer, store hooks.HookStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_hooks_list",
		mcpgo.WithDescription("List configured hooks, optionally filtered."),
		mcpgo.WithString("event", mcpgo.Description("Filter by hook event.")),
		mcpgo.WithString("scope", mcpgo.Description("Filter by scope (global, tenant, agent).")),
		mcpgo.WithString("agent_id", mcpgo.Description("Filter by agent UUID.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("Filter by enabled state.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHooksList(store))

	srv.AddTool(mcpgo.NewTool("goclaw_hooks_create",
		mcpgo.WithDescription("Create a new hook."),
		mcpgo.WithObject("config", mcpgo.Required(), mcpgo.Description("Full hook config object (handler_type, event, scope, config, matcher, etc.).")),
	), handleHooksCreate(store))

	srv.AddTool(mcpgo.NewTool("goclaw_hooks_update",
		mcpgo.WithDescription("Apply a partial update to an existing hook."),
		mcpgo.WithString("hook_id", mcpgo.Required(), mcpgo.Description("Hook UUID.")),
		mcpgo.WithObject("updates", mcpgo.Required(), mcpgo.Description("Column→value patch.")),
	), handleHooksUpdate(store))

	srv.AddTool(mcpgo.NewTool("goclaw_hooks_delete",
		mcpgo.WithDescription("Delete a hook. Builtin hooks are read-only and cannot be deleted."),
		mcpgo.WithString("hook_id", mcpgo.Required(), mcpgo.Description("Hook UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleHooksDelete(store))

	srv.AddTool(mcpgo.NewTool("goclaw_hooks_toggle",
		mcpgo.WithDescription("Enable or disable a hook."),
		mcpgo.WithString("hook_id", mcpgo.Required(), mcpgo.Description("Hook UUID.")),
		mcpgo.WithBoolean("enabled", mcpgo.Required(), mcpgo.Description("Desired enabled state.")),
	), handleHooksToggle(store))

	srv.AddTool(mcpgo.NewTool("goclaw_hooks_test",
		mcpgo.WithDescription("Dry-run a hook. NOTE: not available on this MCP surface — no dry-run test runner is wired here (see internal/gateway/methods/hooks.go's HookTestRunner, which is only attached to the WS RPC surface); always returns an error."),
		mcpgo.WithObject("config", mcpgo.Required(), mcpgo.Description("Hook config to test.")),
		mcpgo.WithObject("sample_event", mcpgo.Description("Sample event payload.")),
	), handleHooksTest())

	srv.AddTool(mcpgo.NewTool("goclaw_hooks_history",
		mcpgo.WithDescription("Return hook execution history. NOTE: matches the WS RPC twin's Phase 3 MVP stub — always returns an empty list (paginated reads are not yet implemented in HookStore)."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHooksHistory())
}

func handleHooksList(store hooks.HookStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		filter := hooks.ListFilter{}
		args := req.GetArguments()
		if v, ok := args["enabled"].(bool); ok {
			filter.Enabled = &v
		}
		if event := req.GetString("event", ""); event != "" {
			ev := hooks.HookEvent(event)
			filter.Event = &ev
		}
		if scope := req.GetString("scope", ""); scope != "" {
			sc := hooks.Scope(scope)
			filter.Scope = &sc
		}
		if agentID := req.GetString("agent_id", ""); agentID != "" {
			if id, err := uuid.Parse(agentID); err == nil {
				filter.AgentID = &id
			}
		}
		list, err := store.List(ctx, filter)
		if err != nil {
			return toolError("hooks.list", err)
		}
		return jsonToolResult(map[string]any{"hooks": list})
	}
}

// parseMCPHookConfig mirrors internal/gateway/methods/hooks.go's
// parseHookConfigParams — strips caller-controlled identity/provenance
// fields so a caller cannot forge a builtin-tier hook.
func parseMCPHookConfig(raw any) (*hooks.HookConfig, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	var cfg hooks.HookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Metadata == nil {
		cfg.Metadata = map[string]any{}
	}
	if cfg.Config == nil {
		cfg.Config = map[string]any{}
	}
	if cfg.HandlerType == "" || cfg.Event == "" || cfg.Scope == "" {
		return nil, errors.New("handler_type, event, and scope are required")
	}
	cfg.Source = ""
	cfg.ID = uuid.Nil
	cfg.CreatedBy = nil
	cfg.Version = 0
	if len(cfg.AgentIDs) == 0 && cfg.AgentID != nil && *cfg.AgentID != uuid.Nil {
		cfg.AgentIDs = []uuid.UUID{*cfg.AgentID}
	}
	return &cfg, nil
}

func handleHooksCreate(store hooks.HookStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		raw, ok := args["config"]
		if !ok {
			return mcpgo.NewToolResultError("hooks.create: config is required"), nil
		}
		cfg, err := parseMCPHookConfig(raw)
		if err != nil {
			return toolError("hooks.create", err)
		}
		if cfg.Scope == hooks.ScopeGlobal {
			cfg.TenantID = hooks.SentinelTenantID
		}
		// This whole MCP CRUD surface is gated by a single bearer token
		// (gateway.mcp_server_token) equivalent to full admin/master scope —
		// same trust model as the rest of internal/mcp/crud_*.go — so global
		// hook creation is allowed here without a per-caller master-scope check.
		if err := cfg.Validate(edition.Current(), true); err != nil {
			return toolError("hooks.create", err)
		}
		id, err := store.Create(ctx, *cfg)
		if err != nil {
			return toolError("hooks.create", err)
		}
		return jsonToolResult(map[string]string{"hookId": id.String()})
	}
}

// applyHookPatch mirrors internal/gateway/methods/hooks.go's unexported
// helper of the same name.
func applyMCPHookPatch(cur hooks.HookConfig, p map[string]any) hooks.HookConfig {
	if v, ok := p["name"].(string); ok {
		cur.Name = v
	}
	if v, ok := p["agent_ids"]; ok {
		if arr, ok := v.([]any); ok {
			var ids []uuid.UUID
			for _, item := range arr {
				if s, ok := item.(string); ok {
					if id, err := uuid.Parse(s); err == nil {
						ids = append(ids, id)
					}
				}
			}
			cur.AgentIDs = ids
		}
	}
	if v, ok := p["event"].(string); ok && v != "" {
		cur.Event = hooks.HookEvent(v)
	}
	if v, ok := p["scope"].(string); ok && v != "" {
		cur.Scope = hooks.Scope(v)
	}
	if v, ok := p["handler_type"].(string); ok && v != "" {
		cur.HandlerType = hooks.HandlerType(v)
	}
	if v, ok := p["matcher"].(string); ok {
		cur.Matcher = v
	}
	if v, ok := p["if_expr"].(string); ok {
		cur.IfExpr = v
	}
	if v, ok := p["timeout_ms"].(float64); ok {
		cur.TimeoutMS = int(v)
	}
	if v, ok := p["on_timeout"].(string); ok && v != "" {
		cur.OnTimeout = hooks.Decision(v)
	}
	if v, ok := p["priority"].(float64); ok {
		cur.Priority = int(v)
	}
	if v, ok := p["enabled"].(bool); ok {
		cur.Enabled = v
	}
	if v, ok := p["config"].(map[string]any); ok {
		cur.Config = v
	}
	if v, ok := p["metadata"].(map[string]any); ok {
		cur.Metadata = v
	}
	return cur
}

func handleHooksUpdate(store hooks.HookStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		hookIDStr, err := req.RequireString("hook_id")
		if err != nil {
			return toolError("hooks.update", err)
		}
		id, err := uuid.Parse(hookIDStr)
		if err != nil {
			return toolError("hooks.update", fmt.Errorf("invalid hook_id: %w", err))
		}
		args := req.GetArguments()
		updates, _ := args["updates"].(map[string]any)
		if len(updates) == 0 {
			return mcpgo.NewToolResultError("hooks.update: updates is required"), nil
		}
		delete(updates, "id")
		delete(updates, "tenant_id")
		delete(updates, "version")
		delete(updates, "source")
		delete(updates, "created_by")

		current, err := store.GetByID(ctx, id)
		if err != nil || current == nil {
			return mcpgo.NewToolResultError("hooks.update: hook not found: " + hookIDStr), nil
		}
		merged := applyMCPHookPatch(*current, updates)
		if err := merged.Validate(edition.Current(), true); err != nil {
			return toolError("hooks.update", err)
		}

		if err := store.Update(ctx, id, updates); err != nil {
			if errors.Is(err, hooks.ErrBuiltinReadOnly) {
				return mcpgo.NewToolResultError("hooks.update: builtin hooks are read-only"), nil
			}
			return toolError("hooks.update", err)
		}
		return jsonToolResult(map[string]string{"hookId": id.String()})
	}
}

func handleHooksDelete(store hooks.HookStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		hookIDStr, err := req.RequireString("hook_id")
		if err != nil {
			return toolError("hooks.delete", err)
		}
		id, err := uuid.Parse(hookIDStr)
		if err != nil {
			return toolError("hooks.delete", fmt.Errorf("invalid hook_id: %w", err))
		}
		if err := store.Delete(ctx, id); err != nil {
			if errors.Is(err, hooks.ErrBuiltinReadOnly) {
				return mcpgo.NewToolResultError("hooks.delete: builtin hooks are read-only"), nil
			}
			return toolError("hooks.delete", err)
		}
		return jsonToolResult(map[string]string{"hookId": id.String()})
	}
}

func handleHooksToggle(store hooks.HookStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		hookIDStr, err := req.RequireString("hook_id")
		if err != nil {
			return toolError("hooks.toggle", err)
		}
		id, err := uuid.Parse(hookIDStr)
		if err != nil {
			return toolError("hooks.toggle", fmt.Errorf("invalid hook_id: %w", err))
		}
		enabled, err := req.RequireBool("enabled")
		if err != nil {
			return toolError("hooks.toggle", err)
		}
		if err := store.Update(ctx, id, map[string]any{"enabled": enabled}); err != nil {
			return toolError("hooks.toggle", err)
		}
		return jsonToolResult(map[string]any{"hookId": id.String(), "enabled": enabled})
	}
}

func handleHooksTest() mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError("hooks.test: dry-run test runner is not available on this MCP surface"), nil
	}
}

func handleHooksHistory() mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return jsonToolResult(map[string]any{
			"executions": []any{},
			"nextCursor": "",
			"note":       "history pagination is not yet implemented in HookStore",
		})
	}
}
