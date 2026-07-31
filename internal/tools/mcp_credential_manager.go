package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MCPCredentialManagerTool allows users to view and manage their MCP server credentials
// from Telegram and other channels. Supports listing accessible servers, checking
// credential status, setting credentials (API key or Bearer token), and deleting credentials.
type MCPCredentialManagerTool struct {
	mcpStore store.MCPServerStore
}

func NewMCPCredentialManagerTool() *MCPCredentialManagerTool {
	return &MCPCredentialManagerTool{}
}

func (t *MCPCredentialManagerTool) SetMCPServerStore(s store.MCPServerStore) {
	t.mcpStore = s
}

func (t *MCPCredentialManagerTool) Name() string {
	return "mcp_credential_manager"
}

func (t *MCPCredentialManagerTool) Description() string {
	return "View and manage your MCP server credentials. Lets you list MCP servers accessible to you, check whether you have credentials configured, set new credentials (API key, Bearer token, headers, environment variables), or delete existing credentials for an MCP server."
}

func (t *MCPCredentialManagerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: list_servers (list accessible MCP servers and credential status), credential_status (check if you have credentials for a server), set_credentials (set/update your API key, headers, and/or env vars for a server), set_bearer_token (set a Bearer token as your credential for a server), delete_credentials (remove your credentials for a server).",
				"enum":        []string{"list_servers", "credential_status", "set_credentials", "set_bearer_token", "delete_credentials"},
			},
			"server_name": map[string]any{
				"type":        "string",
				"description": "Name of the MCP server (required for credential_status, set_credentials, set_bearer_token, delete_credentials).",
			},
			"token": map[string]any{
				"type":        "string",
				"description": "Bearer token for the MCP server (only for set_bearer_token action). The token is stored and sent as an Authorization: Bearer <token> header.",
			},
			"api_key": map[string]any{
				"type":        "string",
				"description": "API key for the MCP server (only for set_credentials action).",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "Optional additional HTTP headers as a JSON object (only for set_credentials action).",
				"additionalProperties": map[string]any{"type": "string"},
			},
			"env": map[string]any{
				"type":        "object",
				"description": "Optional environment variables as a JSON object (only for set_credentials action).",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required": []string{"action"},
	}
}

func (t *MCPCredentialManagerTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.mcpStore == nil {
		return ErrorResult("mcp_credential_manager: MCP server store not available")
	}

	action := argString(args, "action")
	if action == "" {
		return ErrorResult("action is required (list_servers, credential_status, set_credentials, delete_credentials)")
	}

	switch action {
	case "list_servers":
		return t.listServers(ctx)
	case "credential_status":
		return t.credentialStatus(ctx, args)
	case "set_credentials":
		return t.setCredentials(ctx, args)
	case "set_bearer_token":
		return t.setBearerToken(ctx, args)
	case "delete_credentials":
		return t.deleteCredentials(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unsupported action: %s (use list_servers, credential_status, set_credentials, set_bearer_token, delete_credentials)", action))
	}
}

func (t *MCPCredentialManagerTool) listServers(ctx context.Context) *Result {
	agentID := store.AgentIDFromContext(ctx)
	userID := store.UserIDFromContext(ctx)

	accessible, err := t.mcpStore.ListAccessible(ctx, agentID, userID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list MCP servers: %v", err))
	}

	if len(accessible) == 0 {
		return NewResult("You don't have any MCP servers accessible.")
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("**MCP Servers Accessible to You (%d):**", len(accessible)))
	lines = append(lines, "")

	for _, info := range accessible {
		srv := info.Server
		needsCreds := srv.RequireUserCredentials || requireUserCredsFromSettings(srv.Settings)

		// Check if user has credentials for this server
		hasCreds := false
		if userID != "" {
			creds, err := t.mcpStore.GetUserCredentials(ctx, srv.ID, userID)
			if err == nil && creds != nil {
				hasCreds = creds.APIKey != "" || len(creds.Headers) > 0 || len(creds.Env) > 0
			}
		}

		displayName := srv.Name
		if srv.DisplayName != "" {
			displayName = srv.DisplayName
		}

		credsStatus := "✅ credentials set"
		if needsCreds && !hasCreds {
			credsStatus = "⚠️  credentials required - not set"
		} else if !needsCreds && !hasCreds {
			credsStatus = "no credentials needed"
		} else if !needsCreds && hasCreds {
			credsStatus = "✅ custom credentials set"
		}

		lines = append(lines, fmt.Sprintf("- **%s** (%s) - %s", displayName, srv.Name, credsStatus))
	}

	return NewResult(strings.Join(lines, "\n"))
}

func (t *MCPCredentialManagerTool) credentialStatus(ctx context.Context, args map[string]any) *Result {
	serverName := argString(args, "server_name")
	if serverName == "" {
		return ErrorResult("server_name is required for credential_status action")
	}

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return ErrorResult("no user identity available in context")
	}

	server, err := t.mcpStore.GetServerByName(ctx, serverName)
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP server %q not found: %v", serverName, err))
	}

	creds, err := t.mcpStore.GetUserCredentials(ctx, server.ID, userID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to check credentials: %v", err))
	}

	needsCreds := server.RequireUserCredentials || requireUserCredsFromSettings(server.Settings)

	displayName := server.Name
	if server.DisplayName != "" {
		displayName = server.DisplayName
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("**MCP Server: %s**", displayName))
	lines = append(lines, fmt.Sprintf(" Server name: `%s`", server.Name))
	if needsCreds {
		lines = append(lines, " This server requires per-user credentials.")
	} else {
		lines = append(lines, " This server does not require per-user credentials (server-level credentials are configured).")
	}

	if creds == nil || (creds.APIKey == "" && len(creds.Headers) == 0 && len(creds.Env) == 0) {
		lines = append(lines, "")
		lines = append(lines, "**You have not set any credentials for this server.**")
		if needsCreds {
			lines = append(lines, "Use the `set_credentials` action to configure your API key.")
		}
	} else {
		lines = append(lines, "")
		lines = append(lines, "**Your credentials:**")
		if creds.APIKey != "" {
			lines = append(lines, fmt.Sprintf(" - API key: `%s`", maskString(creds.APIKey)))
		}
		if len(creds.Headers) > 0 {
			headerKeys := make([]string, 0, len(creds.Headers))
			for k := range creds.Headers {
				headerKeys = append(headerKeys, k)
			}
			lines = append(lines, fmt.Sprintf(" - Headers: %s", strings.Join(headerKeys, ", ")))
		}
		if len(creds.Env) > 0 {
			envKeys := make([]string, 0, len(creds.Env))
			for k := range creds.Env {
				envKeys = append(envKeys, k)
			}
			lines = append(lines, fmt.Sprintf(" - Env vars: %s", strings.Join(envKeys, ", ")))
		}
	}

	return NewResult(strings.Join(lines, "\n"))
}

func (t *MCPCredentialManagerTool) setCredentials(ctx context.Context, args map[string]any) *Result {
	serverName := argString(args, "server_name")
	if serverName == "" {
		return ErrorResult("server_name is required for set_credentials action")
	}

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return ErrorResult("no user identity available in context")
	}

	server, err := t.mcpStore.GetServerByName(ctx, serverName)
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP server %q not found: %v", serverName, err))
	}

	apiKey := argString(args, "api_key")

	var headers map[string]string
	if rawHeaders, ok := args["headers"].(map[string]any); ok && len(rawHeaders) > 0 {
		headers = make(map[string]string, len(rawHeaders))
		for k, v := range rawHeaders {
			if vs, ok := v.(string); ok {
				headers[k] = vs
			}
		}
	}

	var env map[string]string
	if rawEnv, ok := args["env"].(map[string]any); ok && len(rawEnv) > 0 {
		env = make(map[string]string, len(rawEnv))
		for k, v := range rawEnv {
			if vs, ok := v.(string); ok {
				env[k] = vs
			}
		}
	}

	if apiKey == "" && len(headers) == 0 && len(env) == 0 {
		return ErrorResult("at least one of api_key, headers, or env must be provided")
	}

	creds := store.MCPUserCredentials{
		APIKey:  apiKey,
		Headers: headers,
		Env:     env,
	}

	if err := t.mcpStore.SetUserCredentials(ctx, server.ID, userID, creds); err != nil {
		return ErrorResult(fmt.Sprintf("failed to set credentials: %v", err))
	}

	displayName := server.Name
	if server.DisplayName != "" {
		displayName = server.DisplayName
	}

	var parts []string
	if apiKey != "" {
		parts = append(parts, "API key")
	}
	if len(headers) > 0 {
		parts = append(parts, fmt.Sprintf("%d header(s)", len(headers)))
	}
	if len(env) > 0 {
		parts = append(parts, fmt.Sprintf("%d env var(s)", len(env)))
	}

	return NewResult(fmt.Sprintf("Successfully set credentials (%s) for MCP server **%s**.", strings.Join(parts, ", "), displayName))
}

func (t *MCPCredentialManagerTool) setBearerToken(ctx context.Context, args map[string]any) *Result {
	serverName := argString(args, "server_name")
	if serverName == "" {
		return ErrorResult("server_name is required for set_bearer_token action")
	}

	token := argString(args, "token")
	if token == "" {
		return ErrorResult("token is required for set_bearer_token action")
	}

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return ErrorResult("no user identity available in context")
	}

	server, err := t.mcpStore.GetServerByName(ctx, serverName)
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP server %q not found: %v", serverName, err))
	}

	creds := store.MCPUserCredentials{
		APIKey: token,
	}

	if err := t.mcpStore.SetUserCredentials(ctx, server.ID, userID, creds); err != nil {
		return ErrorResult(fmt.Sprintf("failed to set Bearer token: %v", err))
	}

	displayName := server.Name
	if server.DisplayName != "" {
		displayName = server.DisplayName
	}

	return NewResult(fmt.Sprintf("Successfully set Bearer token for MCP server **%s**. The token will be sent as an Authorization: Bearer header.", displayName))
}

func (t *MCPCredentialManagerTool) deleteCredentials(ctx context.Context, args map[string]any) *Result {
	serverName := argString(args, "server_name")
	if serverName == "" {
		return ErrorResult("server_name is required for delete_credentials action")
	}

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return ErrorResult("no user identity available in context")
	}

	server, err := t.mcpStore.GetServerByName(ctx, serverName)
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP server %q not found: %v", serverName, err))
	}

	if err := t.mcpStore.DeleteUserCredentials(ctx, server.ID, userID); err != nil {
		return ErrorResult(fmt.Sprintf("failed to delete credentials: %v", err))
	}

	displayName := server.Name
	if server.DisplayName != "" {
		displayName = server.DisplayName
	}

	return NewResult(fmt.Sprintf("Successfully deleted credentials for MCP server **%s**.", displayName))
}

// maskString returns a masked version of a sensitive string for display.
// Shows first 4 and last 4 characters, masking the middle.
func maskString(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

// requireUserCredsFromSettings checks if an MCP server's settings mandate per-user credentials.
func requireUserCredsFromSettings(settings json.RawMessage) bool {
	if len(settings) == 0 {
		return false
	}
	var s struct {
		RequireUserCredentials bool `json:"require_user_credentials"`
	}
	_ = json.Unmarshal(settings, &s)
	return s.RequireUserCredentials
}

// Ensure MCPCredentialManagerTool implements MCPServerStoreAware.
var _ MCPServerStoreAware = (*MCPCredentialManagerTool)(nil)

// Ensure MCPCredentialManagerTool implements Tool.
var _ Tool = (*MCPCredentialManagerTool)(nil)
