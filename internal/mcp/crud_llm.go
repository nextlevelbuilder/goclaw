package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// LLMDefaults carries the gateway's background-provider fallback used when a
// goclaw_llm_complete call does not specify a provider/model, mirroring
// internal/gateway/methods/llm.go's llmDefaults.
type LLMDefaults struct {
	Provider string
	Model    string
}

// registerLLMCRUDTool registers goclaw_llm_complete, backed by the same
// providers.Registry the gateway's own llm.complete WS method uses. The WS
// method additionally requires RoleOperator; this MCP surface has no
// per-caller role (the bearer token is the sole boundary), matching the rest
// of this CRUD MCP server.
func registerLLMCRUDTool(srv *mcpserver.MCPServer, reg *providers.Registry, defaults LLMDefaults) {
	srv.AddTool(mcpgo.NewTool("goclaw_llm_complete",
		mcpgo.WithDescription("Request a one-shot LLM completion via the gateway's configured provider registry, bypassing the agent loop."),
		mcpgo.WithString("provider", mcpgo.Description("Provider name (e.g. \"anthropic\"); defaults to the gateway's background provider.")),
		mcpgo.WithString("model", mcpgo.Description("Model name; defaults to the gateway's background model or the provider's default.")),
		mcpgo.WithArray("messages", mcpgo.Required(), mcpgo.Description("Chat messages: [{role, content}, ...].")),
		mcpgo.WithNumber("temperature", mcpgo.Description("Sampling temperature.")),
		mcpgo.WithNumber("max_tokens", mcpgo.Description("Max completion tokens.")),
	), handleLLMComplete(reg, defaults))
}

type llmCompleteMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func handleLLMComplete(reg *providers.Registry, defaults LLMDefaults) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if reg == nil {
			return mcpgo.NewToolResultError("llm.complete: no providers configured"), nil
		}

		args := req.GetArguments()
		rawMessages, _ := args["messages"].([]any)
		if len(rawMessages) == 0 {
			return mcpgo.NewToolResultError("llm.complete: messages is required"), nil
		}
		messages := make([]providers.Message, 0, len(rawMessages))
		for i, raw := range rawMessages {
			obj, ok := raw.(map[string]any)
			if !ok {
				return mcpgo.NewToolResultError(fmt.Sprintf("llm.complete: messages[%d] must be an object", i)), nil
			}
			role, _ := obj["role"].(string)
			content, _ := obj["content"].(string)
			if strings.TrimSpace(role) == "" {
				return mcpgo.NewToolResultError(fmt.Sprintf("llm.complete: messages[%d].role is required", i)), nil
			}
			if strings.TrimSpace(content) == "" {
				return mcpgo.NewToolResultError(fmt.Sprintf("llm.complete: messages[%d].content is required", i)), nil
			}
			messages = append(messages, providers.Message{Role: role, Content: content})
		}

		providerName := strings.TrimSpace(req.GetString("provider", ""))
		if providerName == "" {
			providerName = strings.TrimSpace(defaults.Provider)
		}
		prov, model, err := resolveLLMProvider(ctx, reg, defaults, providerName, strings.TrimSpace(req.GetString("model", "")))
		if err != nil {
			return toolError("llm.complete", err)
		}

		options := map[string]any{}
		if maxTokens := int(req.GetFloat("max_tokens", 0)); maxTokens > 0 {
			options[providers.OptMaxTokens] = maxTokens
		}
		if temp, ok := args["temperature"].(float64); ok {
			options[providers.OptTemperature] = temp
		}

		resp, err := prov.Chat(ctx, providers.ChatRequest{
			Messages: messages,
			Model:    model,
			Options:  options,
		})
		if err != nil {
			return toolError("llm.complete", err)
		}

		result := map[string]any{
			"provider": prov.Name(),
			"model":    model,
			"content":  resp.Content,
		}
		if resp.Usage != nil {
			result["usage"] = resp.Usage
		}
		return jsonToolResult(result)
	}
}

func resolveLLMProvider(ctx context.Context, reg *providers.Registry, defaults LLMDefaults, providerName, model string) (providers.Provider, string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = providers.MasterTenantID
	}

	try := func(name string) (providers.Provider, string, bool) {
		if name == "" {
			return nil, "", false
		}
		p, err := reg.GetForTenant(tenantID, name)
		if err != nil || p == nil {
			return nil, "", false
		}
		selectedModel := model
		if selectedModel == "" {
			selectedModel = strings.TrimSpace(defaults.Model)
		}
		if selectedModel == "" {
			selectedModel = p.DefaultModel()
		}
		return p, selectedModel, true
	}

	if p, selectedModel, ok := try(providerName); ok {
		return p, selectedModel, nil
	}
	if providerName != "" {
		return nil, "", fmt.Errorf("provider not found: %s", providerName)
	}
	for _, name := range reg.ListForTenant(tenantID) {
		if p, selectedModel, ok := try(name); ok {
			return p, selectedModel, nil
		}
	}
	return nil, "", fmt.Errorf("no providers configured")
}
