package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerProvidersCRUDTools registers the goclaw_providers_* MCP tools
// backed by store.ProviderStore — closes a CLI-vs-MCP coverage gap (the
// `goclaw providers create/list/models/verify-embedding` commands had no
// MCP equivalent for basic CRUD). deps.Providers was already threaded
// through CRUDDeps for heartbeat.set's provider-name resolution; this
// reuses the same store reference.
func registerProvidersCRUDTools(srv *mcpserver.MCPServer, providers store.ProviderStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_providers_list",
		mcpgo.WithDescription("List all LLM providers (API keys masked)."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleProvidersList(providers))

	srv.AddTool(mcpgo.NewTool("goclaw_providers_get",
		mcpgo.WithDescription("Get a single LLM provider by UUID or name (API key masked)."),
		mcpgo.WithString("id", mcpgo.Description("Provider UUID.")),
		mcpgo.WithString("name", mcpgo.Description("Provider name, used when id is not known.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleProvidersGet(providers))

	srv.AddTool(mcpgo.NewTool("goclaw_providers_create",
		mcpgo.WithDescription("Register a new LLM provider. The API key is encrypted at rest and never echoed back."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Provider name (unique per tenant).")),
		mcpgo.WithString("provider_type", mcpgo.Required(), mcpgo.Description("Provider type (e.g. \"anthropic\", \"openai\", \"dashscope\").")),
		mcpgo.WithString("display_name", mcpgo.Description("Human-readable display name; defaults to name.")),
		mcpgo.WithString("api_base", mcpgo.Description("API base URL override.")),
		mcpgo.WithString("api_key", mcpgo.Description("API key; stored encrypted.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("Enabled state; defaults to true.")),
	), handleProvidersCreate(providers))

	srv.AddTool(mcpgo.NewTool("goclaw_providers_update",
		mcpgo.WithDescription("Apply a partial update to an existing LLM provider."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Provider UUID.")),
		mcpgo.WithString("display_name", mcpgo.Description("New display name.")),
		mcpgo.WithString("api_base", mcpgo.Description("New API base URL.")),
		mcpgo.WithString("api_key", mcpgo.Description("New API key; stored encrypted.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("New enabled state.")),
	), handleProvidersUpdate(providers))

	srv.AddTool(mcpgo.NewTool("goclaw_providers_delete",
		mcpgo.WithDescription("Delete an LLM provider by UUID."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Provider UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleProvidersDelete(providers))
}

// maskProviderAPIKey mirrors internal/http/providers.go's unexported
// maskAPIKey — duplicated because this MCP surface does not depend on
// internal/http (internal/http already imports internal/mcp, so the
// reverse import would cycle). Replaces a non-empty key with "***" so raw
// secrets never leave this process via an MCP tool response.
func maskProviderAPIKey(p *store.LLMProviderData) {
	if p.APIKey != "" {
		p.APIKey = "***"
	}
}

func handleProvidersList(providers store.ProviderStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list, err := providers.ListProviders(ctx)
		if err != nil {
			return toolError("providers.list", err)
		}
		for i := range list {
			maskProviderAPIKey(&list[i])
		}
		return jsonToolResult(list)
	}
}

func handleProvidersGet(providers store.ProviderStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr := req.GetString("id", "")
		name := req.GetString("name", "")
		switch {
		case idStr != "":
			id, err := uuid.Parse(idStr)
			if err != nil {
				return toolError("providers.get", fmt.Errorf("invalid id: %w", err))
			}
			p, err := providers.GetProvider(ctx, id)
			if err != nil {
				return toolError("providers.get", err)
			}
			maskProviderAPIKey(p)
			return jsonToolResult(p)
		case name != "":
			p, err := providers.GetProviderByName(ctx, name)
			if err != nil {
				return toolError("providers.get", err)
			}
			maskProviderAPIKey(p)
			return jsonToolResult(p)
		default:
			return mcpgo.NewToolResultError("providers.get: one of id or name is required"), nil
		}
	}
}

func handleProvidersCreate(providers store.ProviderStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("providers.create", err)
		}
		providerType, err := req.RequireString("provider_type")
		if err != nil {
			return toolError("providers.create", err)
		}
		p := &store.LLMProviderData{
			Name:         name,
			DisplayName:  req.GetString("display_name", name),
			ProviderType: providerType,
			APIBase:      req.GetString("api_base", ""),
			APIKey:       req.GetString("api_key", ""),
			Enabled:      req.GetBool("enabled", true),
		}
		if err := providers.CreateProvider(ctx, p); err != nil {
			return toolError("providers.create", err)
		}
		maskProviderAPIKey(p)
		return jsonToolResult(p)
	}
}

func handleProvidersUpdate(providers store.ProviderStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("providers.update", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("providers.update", fmt.Errorf("invalid id: %w", err))
		}

		updates := map[string]any{}
		args := req.GetArguments()
		if v, ok := args["display_name"]; ok {
			updates["display_name"] = v
		}
		if v, ok := args["api_base"]; ok {
			updates["api_base"] = v
		}
		if v, ok := args["api_key"]; ok {
			updates["api_key"] = v
		}
		if v, ok := args["enabled"]; ok {
			updates["enabled"] = v
		}
		if len(updates) == 0 {
			return mcpgo.NewToolResultError("providers.update: no fields to update"), nil
		}

		if err := providers.UpdateProvider(ctx, id, updates); err != nil {
			return toolError("providers.update", err)
		}
		p, err := providers.GetProvider(ctx, id)
		if err != nil {
			return toolError("providers.update", err)
		}
		maskProviderAPIKey(p)
		return jsonToolResult(p)
	}
}

func handleProvidersDelete(providers store.ProviderStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("providers.delete", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("providers.delete", fmt.Errorf("invalid id: %w", err))
		}
		if err := providers.DeleteProvider(ctx, id); err != nil {
			return toolError("providers.delete", err)
		}
		return jsonToolResult(map[string]bool{"deleted": true})
	}
}
