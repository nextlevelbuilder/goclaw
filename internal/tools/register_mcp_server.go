package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// RegisterMCPServerTool registers an MCP server in the database,
// making its tools available to agents.
type RegisterMCPServerTool struct {
	mcpStore store.MCPServerStore
	encKey   string // AES-256 encryption key
}

func NewRegisterMCPServerTool(mcpStore store.MCPServerStore, encKey string) *RegisterMCPServerTool {
	return &RegisterMCPServerTool{mcpStore: mcpStore, encKey: encKey}
}

func (t *RegisterMCPServerTool) Name() string { return "register_mcp_server" }

func (t *RegisterMCPServerTool) Description() string {
	return "Register an MCP server in GoClaw so its tools become available to agents. " +
		"Use after building an MCP server with the mcp-builder skill. " +
		"Supports stdio (local), sse, and streamable-http (remote) transports. " +
		"The server is auto-granted to the calling agent."
}

func (t *RegisterMCPServerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Unique server name (e.g., 'github-mcp', 'slack-mcp')",
			},
			"transport": map[string]any{
				"type":        "string",
				"enum":        []string{"stdio", "sse", "streamable-http"},
				"description": "Transport type: stdio for local servers, sse or streamable-http for remote",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Command to run the server (stdio only, e.g., 'node', 'python')",
			},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command arguments (stdio only, e.g., ['dist/index.js'])",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Server URL (sse/streamable-http only)",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "HTTP headers, e.g., Authorization (encrypted at rest)",
			},
			"env": map[string]any{
				"type":        "object",
				"description": "Environment variables for the server process (stdio only, encrypted at rest)",
			},
			"api_key": map[string]any{
				"type":        "string",
				"description": "API key for the server (encrypted at rest via AES-256-GCM)",
			},
			"display_name": map[string]any{
				"type":        "string",
				"description": "Human-friendly display name for the server",
			},
			"tool_prefix": map[string]any{
				"type":        "string",
				"description": "Prefix added to all tool names from this server",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Connection timeout in seconds (default: 30)",
			},
		},
		"required": []string{"name", "transport"},
	}
}

func (t *RegisterMCPServerTool) Execute(ctx context.Context, args map[string]any) *Result {
	name, _ := args["name"].(string)
	transport, _ := args["transport"].(string)

	if name == "" {
		return ErrorResult("name is required")
	}
	if transport == "" {
		return ErrorResult("transport is required")
	}

	// Normalize name
	name = strings.ToLower(strings.TrimSpace(name))

	// Validate transport-specific fields
	switch transport {
	case "stdio":
		cmd, _ := args["command"].(string)
		if cmd == "" {
			return ErrorResult("command is required for stdio transport")
		}
	case "sse", "streamable-http":
		url, _ := args["url"].(string)
		if url == "" {
			return ErrorResult("url is required for " + transport + " transport")
		}
	default:
		return ErrorResult(fmt.Sprintf("unsupported transport %q: must be stdio, sse, or streamable-http", transport))
	}

	// Check name conflict
	existing, err := t.mcpStore.GetServerByName(ctx, name)
	if err == nil && existing != nil {
		return ErrorResult(fmt.Sprintf("MCP server with name %q already exists (ID: %s). Use a different name or update the existing server via the web UI.", name, existing.ID))
	}

	// Build server data
	srv := &store.MCPServerData{
		Name:      name,
		Transport: transport,
		Enabled:   true,
	}

	// Optional string fields
	if v, ok := args["command"].(string); ok {
		srv.Command = v
	}
	if v, ok := args["url"].(string); ok {
		srv.URL = v
	}
	if v, ok := args["api_key"].(string); ok && v != "" {
		if t.encKey != "" {
			encrypted, encErr := crypto.Encrypt(v, t.encKey)
			if encErr != nil {
				return ErrorResult(fmt.Sprintf("failed to encrypt api_key: %v", encErr))
			}
			srv.APIKey = encrypted
		} else {
			srv.APIKey = v
		}
	}
	if v, ok := args["display_name"].(string); ok {
		srv.DisplayName = v
	}
	if v, ok := args["tool_prefix"].(string); ok {
		srv.ToolPrefix = v
	}

	// Timeout
	srv.TimeoutSec = 30
	if v, ok := args["timeout_sec"].(float64); ok && v > 0 {
		srv.TimeoutSec = int(v)
	}

	// JSON fields: args, headers, env
	if v, ok := args["args"]; ok {
		if raw, jsonErr := json.Marshal(v); jsonErr == nil {
			srv.Args = raw
		}
	}
	if v, ok := args["headers"]; ok {
		if raw, jsonErr := json.Marshal(v); jsonErr == nil {
			srv.Headers = raw
		}
	}
	if v, ok := args["env"]; ok {
		if raw, jsonErr := json.Marshal(v); jsonErr == nil {
			srv.Env = raw
		}
	}

	// Set owner
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		userID = "system"
	}
	srv.CreatedBy = userID

	// Create in database
	if err := t.mcpStore.CreateServer(ctx, srv); err != nil {
		return ErrorResult(fmt.Sprintf("failed to register MCP server: %v", err))
	}

	slog.Info("mcp server registered via tool", "id", srv.ID, "name", name, "transport", transport, "owner", userID)

	// Auto-grant to calling agent
	agentID := store.AgentIDFromContext(ctx)
	if agentID != uuid.Nil {
		grant := &store.MCPAgentGrant{
			ServerID:  srv.ID,
			AgentID:   agentID,
			Enabled:   true,
			GrantedBy: userID,
		}
		if grantErr := t.mcpStore.GrantToAgent(ctx, grant); grantErr != nil {
			slog.Warn("register_mcp_server: auto-grant failed", "error", grantErr)
		}
	}

	// Build result message
	result := fmt.Sprintf("MCP server %q registered successfully.\n- ID: %s\n- Transport: %s\n- Name: %s",
		name, srv.ID, transport, name)
	if srv.DisplayName != "" {
		result += "\n- Display Name: " + srv.DisplayName
	}
	if agentID != uuid.Nil {
		result += "\n- Granted to current agent"
	}
	result += "\n\nThe server's tools will be available on the next agent turn. " +
		"To grant access to other agents, use the MCP Servers page in the web dashboard."

	return NewResult(result)
}
