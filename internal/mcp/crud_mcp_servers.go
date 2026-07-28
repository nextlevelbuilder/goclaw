package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// mcpServerCRUDDeps bundles what the goclaw_mcp_servers_* tools need. Only
// Servers is required; the rest degrade gracefully (see each handler).
type mcpServerCRUDDeps struct {
	Servers store.MCPServerStore
	// Manager, when set, lets goclaw_mcp_servers_tools return the tool set of
	// an already-connected server without a fresh handshake.
	Manager *Manager
	// Pool, when set, is evicted on reconnect and on credential-affecting
	// updates so the next call reconnects with the new configuration.
	Pool *Pool
	// OAuth, when set, supplies Bearer tokens for discovery against
	// OAuth-protected servers. When nil, such servers are never discovered
	// with their static headers — see handleMCPServersTools.
	OAuth OAuthTokenProvider
	// MessageBus, when set, broadcasts the same cache-invalidate event the
	// REST handlers emit, so live agents pick up server changes.
	MessageBus *bus.MessageBus
}

// mcpServerNameRe mirrors internal/http's slugRe: MCP server names become tool
// name prefixes, so the same character restriction applies here. Duplicated
// rather than imported because internal/http already imports this package.
var mcpServerNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// mcpServerUpdatableFields mirrors internal/http's mcpServerAllowedFields —
// the allowlist of columns an update may touch. Same defense-in-depth intent:
// an unknown key is dropped rather than reaching the store.
var mcpServerUpdatableFields = map[string]bool{
	"name": true, "display_name": true, "transport": true, "command": true,
	"args": true, "url": true, "api_key": true, "env": true, "headers": true,
	"enabled": true, "tool_prefix": true, "timeout_sec": true,
	"require_user_credentials": true, "settings": true,
}

// registerMCPServerCRUDTools registers the goclaw_mcp_servers_* MCP tools,
// closing the gap that made MCP-server management the one admin surface
// reachable only over REST (/v1/mcp/servers): an agent could mint an API key
// with goclaw_api_keys_create but then had to leave MCP to register a server.
func registerMCPServerCRUDTools(srv *mcpserver.MCPServer, deps mcpServerCRUDDeps) {
	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_list",
		mcpgo.WithDescription("List configured MCP servers with their agent grant counts. Credentials (api_key, header and env values) are masked."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMCPServersList(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_get",
		mcpgo.WithDescription("Get a single MCP server by UUID or name. Credentials are masked."),
		mcpgo.WithString("id", mcpgo.Description("Server UUID.")),
		mcpgo.WithString("name", mcpgo.Description("Server name, used when id is not known.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMCPServersGet(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_create",
		mcpgo.WithDescription("Register a new MCP server. Use transport \"stdio\" with command+args, or \"http\"/\"sse\" with url. Credentials are stored encrypted and never echoed back."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Server name: lowercase alphanumerics and dashes; becomes the tool name prefix.")),
		mcpgo.WithString("transport", mcpgo.Required(), mcpgo.Description("Transport: \"stdio\", \"http\", \"streamable-http\" or \"sse\".")),
		mcpgo.WithString("display_name", mcpgo.Description("Human-readable display name; defaults to name.")),
		mcpgo.WithString("command", mcpgo.Description("Executable to run (stdio transport only).")),
		mcpgo.WithArray("args", mcpgo.Description("Arguments passed to command (stdio transport only)."), mcpgo.WithStringItems()),
		mcpgo.WithString("url", mcpgo.Description("Server URL (http/sse transports only).")),
		mcpgo.WithObject("headers", mcpgo.Description("HTTP headers sent on every request, as a string→string object.")),
		mcpgo.WithObject("env", mcpgo.Description("Environment variables for the child process, as a string→string object.")),
		mcpgo.WithString("api_key", mcpgo.Description("API key sent as the Authorization bearer token; stored encrypted.")),
		mcpgo.WithString("tool_prefix", mcpgo.Description("Prefix prepended to this server's tool names; defaults to name.")),
		mcpgo.WithNumber("timeout_sec", mcpgo.Description("Per-call timeout in seconds.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("Enabled state; defaults to true.")),
		mcpgo.WithBoolean("require_user_credentials", mcpgo.Description("Mint credentials per user at message time instead of sharing one admin key.")),
		mcpgo.WithObject("settings", mcpgo.Description("Extra settings blob (e.g. an \"oauth\" block).")),
	), handleMCPServersCreate(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_update",
		mcpgo.WithDescription("Apply a partial update to an MCP server. Changing url, credentials or the oauth settings evicts pooled connections so the next call reconnects."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Server UUID.")),
		mcpgo.WithString("name", mcpgo.Description("New name: lowercase alphanumerics and dashes.")),
		mcpgo.WithString("display_name", mcpgo.Description("New display name.")),
		mcpgo.WithString("transport", mcpgo.Description("New transport.")),
		mcpgo.WithString("command", mcpgo.Description("New command (stdio transport only).")),
		mcpgo.WithArray("args", mcpgo.Description("New argument list (stdio transport only)."), mcpgo.WithStringItems()),
		mcpgo.WithString("url", mcpgo.Description("New server URL (http/sse transports only).")),
		mcpgo.WithObject("headers", mcpgo.Description("Replacement HTTP headers object.")),
		mcpgo.WithObject("env", mcpgo.Description("Replacement environment object.")),
		mcpgo.WithString("api_key", mcpgo.Description("New API key; stored encrypted.")),
		mcpgo.WithString("tool_prefix", mcpgo.Description("New tool name prefix.")),
		mcpgo.WithNumber("timeout_sec", mcpgo.Description("New per-call timeout in seconds.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("New enabled state.")),
		mcpgo.WithBoolean("require_user_credentials", mcpgo.Description("New per-user credential requirement.")),
		mcpgo.WithObject("settings", mcpgo.Description("Replacement settings blob.")),
	), handleMCPServersUpdate(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_delete",
		mcpgo.WithDescription("Delete an MCP server by UUID, along with its grants."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Server UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleMCPServersDelete(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_tools",
		mcpgo.WithDescription("List the tools a configured MCP server exposes, from the live connection when available and by on-demand discovery otherwise."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Server UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMCPServersTools(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_test",
		mcpgo.WithDescription("Test an MCP server configuration by connecting and counting its tools, without saving anything. Pass server_id to test a stored server's OAuth credentials."),
		mcpgo.WithString("transport", mcpgo.Required(), mcpgo.Description("Transport: \"stdio\", \"http\", \"streamable-http\" or \"sse\".")),
		mcpgo.WithString("server_id", mcpgo.Description("UUID of a stored server, so an OAuth token is used instead of the supplied headers.")),
		mcpgo.WithString("command", mcpgo.Description("Executable to run (stdio transport only).")),
		mcpgo.WithArray("args", mcpgo.Description("Arguments passed to command (stdio transport only)."), mcpgo.WithStringItems()),
		mcpgo.WithString("url", mcpgo.Description("Server URL (http/sse transports only).")),
		mcpgo.WithObject("headers", mcpgo.Description("HTTP headers to test with, as a string→string object.")),
		mcpgo.WithObject("env", mcpgo.Description("Environment variables to test with, as a string→string object.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMCPServersTest(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_mcp_servers_reconnect",
		mcpgo.WithDescription("Drop the pooled connection to an MCP server so the next call reconnects with its current configuration."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Server UUID.")),
	), handleMCPServersReconnect(deps))
}

// maskedSecret replaces every credential value leaving this surface. The CRUD
// MCP server is gated by one shared full-trust token, but an MCP tool result is
// fed straight into an LLM context (and its transcript), which is not a place
// raw API keys or Authorization headers should ever land. Matches
// maskProviderAPIKey's reasoning for goclaw_providers_*.
const maskedSecret = "***"

// maskMCPServerSecrets blanks api_key and every header/env value in place,
// keeping the keys so a caller can still see which are set.
func maskMCPServerSecrets(srv *store.MCPServerData) {
	if srv.APIKey != "" {
		srv.APIKey = maskedSecret
	}
	srv.Headers = maskJSONObjectValues(srv.Headers)
	srv.Env = maskJSONObjectValues(srv.Env)
}

// maskJSONObjectValues rewrites a JSONB string→string object so every value
// becomes maskedSecret. A blob that is absent or not a string map is returned
// unchanged: it holds no credential this function knows how to mask, and
// dropping it would silently hide configuration from the caller.
func maskJSONObjectValues(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var obj map[string]string
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	for key := range obj {
		obj[key] = maskedSecret
	}
	masked, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return masked
}

// emitMCPCacheInvalidate mirrors MCPHandler.emitCacheInvalidate so a server
// changed over MCP invalidates the same caches a REST change would. Without
// it, running agents keep serving the previous tool set until they restart.
func (deps mcpServerCRUDDeps) emitMCPCacheInvalidate() {
	if deps.MessageBus == nil {
		return
	}
	deps.MessageBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindMCP},
	})
}

// mcpServerWithGrantCount is the list-tool shape: the stored server plus how
// many agents hold a grant on it, matching GET /v1/mcp/servers.
type mcpServerWithGrantCount struct {
	store.MCPServerData
	AgentCount int `json:"agent_count"`
}

func handleMCPServersList(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		servers, err := deps.Servers.ListServers(ctx)
		if err != nil {
			return toolError("mcp_servers.list", err)
		}
		// A counts failure degrades to zeroes rather than failing the listing:
		// the grant count is decoration, the server list is the answer.
		counts, err := deps.Servers.CountAgentGrantsByServer(ctx)
		if err != nil {
			slog.Warn("mcp_servers.list.count_grants_failed", "error", err)
			counts = map[uuid.UUID]int{}
		}
		result := make([]mcpServerWithGrantCount, len(servers))
		for i := range servers {
			maskMCPServerSecrets(&servers[i])
			result[i] = mcpServerWithGrantCount{
				MCPServerData: servers[i],
				AgentCount:    counts[servers[i].ID],
			}
		}
		return jsonToolResult(map[string]any{"servers": result})
	}
}

func handleMCPServersGet(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		srv, err := lookupMCPServer(ctx, deps.Servers, req.GetString("id", ""), req.GetString("name", ""))
		if err != nil {
			return toolError("mcp_servers.get", err)
		}
		if srv == nil {
			return mcpgo.NewToolResultError("mcp_servers.get: one of id or name is required"), nil
		}
		maskMCPServerSecrets(srv)
		return jsonToolResult(srv)
	}
}

// lookupMCPServer resolves a server by UUID or by name. A (nil, nil) result
// means neither identifier was supplied — the caller reports that, since it is
// a malformed request rather than a lookup failure.
func lookupMCPServer(ctx context.Context, servers store.MCPServerStore, idStr, name string) (*store.MCPServerData, error) {
	switch {
	case idStr != "":
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid id: %w", err)
		}
		srv, err := servers.GetServer(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get server %s: %w", id, err)
		}
		return srv, nil
	case name != "":
		srv, err := servers.GetServerByName(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("get server %q: %w", name, err)
		}
		return srv, nil
	default:
		return nil, nil
	}
}

func handleMCPServersCreate(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("mcp_servers.create", err)
		}
		transport, err := req.RequireString("transport")
		if err != nil {
			return toolError("mcp_servers.create", err)
		}
		if !mcpServerNameRe.MatchString(name) {
			return mcpgo.NewToolResultError("mcp_servers.create: name must be lowercase alphanumerics and dashes"), nil
		}

		args := stringSliceArg(req, "args")
		command := req.GetString("command", "")
		url := req.GetString("url", "")
		// Same guard the REST handler applies: reject shell metacharacters,
		// non-allowlisted commands and private-network URLs before storing a
		// config the bridge would later execute or dial.
		if err := ValidateServerConfig(transport, command, args, url); err != nil {
			slog.Warn("security.mcp.server_rejected", "source", "mcp_crud", "reason", err.Error(), "transport", transport)
			return toolError("mcp_servers.create", err)
		}

		srv := &store.MCPServerData{
			Name:                   name,
			DisplayName:            req.GetString("display_name", name),
			Transport:              transport,
			Command:                command,
			URL:                    url,
			APIKey:                 req.GetString("api_key", ""),
			ToolPrefix:             req.GetString("tool_prefix", ""),
			TimeoutSec:             req.GetInt("timeout_sec", 0),
			Enabled:                req.GetBool("enabled", true),
			RequireUserCredentials: req.GetBool("require_user_credentials", false),
			CreatedBy:              "mcp_crud",
		}
		for _, field := range []struct {
			key    string
			target *json.RawMessage
		}{
			{"args", &srv.Args},
			{"headers", &srv.Headers},
			{"env", &srv.Env},
			{"settings", &srv.Settings},
		} {
			raw, err := rawJSONArg(req, field.key)
			if err != nil {
				return toolError("mcp_servers.create", err)
			}
			*field.target = raw
		}

		if err := deps.Servers.CreateServer(ctx, srv); err != nil {
			return toolError("mcp_servers.create", fmt.Errorf("create server %q: %w", name, err))
		}
		deps.emitMCPCacheInvalidate()
		// Mask a copy: srv is the struct the store just took ownership of, and
		// an in-place mask would overwrite the credentials it holds.
		response := *srv
		maskMCPServerSecrets(&response)
		return jsonToolResult(&response)
	}
}

// stringSliceArg reads an array-of-strings argument, ignoring any non-string
// element. Mirrors the REST update handler's tolerant []any walk.
func stringSliceArg(req mcpgo.CallToolRequest, key string) []string {
	raw, ok := req.GetArguments()[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// rawJSONArg re-marshals a structured argument into the JSONB shape the store
// expects. An absent argument yields nil so the column keeps its default.
func rawJSONArg(req mcpgo.CallToolRequest, key string) (json.RawMessage, error) {
	value, ok := req.GetArguments()[key]
	if !ok || value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", key, err)
	}
	return raw, nil
}

func handleMCPServersUpdate(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("mcp_servers.update", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("mcp_servers.update", fmt.Errorf("invalid id: %w", err))
		}

		updates := map[string]any{}
		for key, value := range req.GetArguments() {
			if mcpServerUpdatableFields[key] {
				updates[key] = value
			}
		}
		if len(updates) == 0 {
			return mcpgo.NewToolResultError("mcp_servers.update: no fields to update"), nil
		}
		if name, ok := updates["name"].(string); ok && !mcpServerNameRe.MatchString(name) {
			return mcpgo.NewToolResultError("mcp_servers.update: name must be lowercase alphanumerics and dashes"), nil
		}

		existing, err := deps.Servers.GetServer(ctx, id)
		if err != nil {
			return toolError("mcp_servers.update", fmt.Errorf("get server %s: %w", id, err))
		}
		// Validate the merged config, not just the changed fields: switching
		// transport without clearing command (or vice versa) is only visible
		// against the stored row.
		if err := ValidateServerConfig(
			stringUpdateOr(updates, "transport", existing.Transport),
			stringUpdateOr(updates, "command", existing.Command),
			mergedArgs(updates, existing),
			stringUpdateOr(updates, "url", existing.URL),
		); err != nil {
			slog.Warn("security.mcp.server_update_rejected", "source", "mcp_crud", "server_id", id, "reason", err.Error())
			return toolError("mcp_servers.update", err)
		}

		if err := deps.Servers.UpdateServer(ctx, id, updates); err != nil {
			return toolError("mcp_servers.update", fmt.Errorf("update server %s: %w", id, err))
		}

		// Anything that changes how the bridge authenticates or where it
		// connects must drop pooled connections, or the old credentials keep
		// being replayed until the process restarts. EvictServer covers the
		// shared connection and every per-user one, since per-user connections
		// inherit the server-level headers/api_key as their base.
		evicted := false
		if deps.Pool != nil && existing.Name != "" && credentialAffectingUpdate(updates) {
			deps.Pool.EvictServer(store.TenantIDFromContext(ctx), existing.Name)
			evicted = true
		}
		deps.emitMCPCacheInvalidate()

		updated, err := deps.Servers.GetServer(ctx, id)
		if err != nil {
			return toolError("mcp_servers.update", fmt.Errorf("get updated server %s: %w", id, err))
		}
		maskMCPServerSecrets(updated)
		return jsonToolResult(map[string]any{"server": updated, "pool_evicted": evicted})
	}
}

// credentialAffectingUpdate reports whether an update changes where the bridge
// connects or how it authenticates, and therefore invalidates pooled
// connections.
func credentialAffectingUpdate(updates map[string]any) bool {
	for _, key := range []string{"url", "api_key", "headers", "env", "settings", "transport", "command", "args"} {
		if _, ok := updates[key]; ok {
			return true
		}
	}
	return false
}

// stringUpdateOr returns the updated string value for key, or the stored value
// when the update does not touch it.
func stringUpdateOr(updates map[string]any, key, current string) string {
	if value, ok := updates[key].(string); ok {
		return value
	}
	return current
}

// mergedArgs returns the argument list an update would leave in place: the new
// one when supplied, the stored one otherwise.
func mergedArgs(updates map[string]any, existing *store.MCPServerData) []string {
	if raw, ok := updates["args"]; ok {
		items, ok := raw.([]any)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	var args []string
	if len(existing.Args) > 0 {
		if err := json.Unmarshal(existing.Args, &args); err != nil {
			slog.Warn("mcp_servers.update.args_decode_failed", "server_id", existing.ID, "error", err)
			return nil
		}
	}
	return args
}

func handleMCPServersDelete(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("mcp_servers.delete", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("mcp_servers.delete", fmt.Errorf("invalid id: %w", err))
		}
		// Read the name first: after deletion there is nothing left to derive
		// the pool key from, and a surviving pooled connection would keep
		// serving a server the operator just removed.
		var name string
		if existing, err := deps.Servers.GetServer(ctx, id); err == nil && existing != nil {
			name = existing.Name
		}
		if err := deps.Servers.DeleteServer(ctx, id); err != nil {
			return toolError("mcp_servers.delete", fmt.Errorf("delete server %s: %w", id, err))
		}
		if deps.Pool != nil && name != "" {
			deps.Pool.EvictServer(store.TenantIDFromContext(ctx), name)
		}
		deps.emitMCPCacheInvalidate()
		return jsonToolResult(map[string]bool{"deleted": true})
	}
}

func handleMCPServersTools(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("mcp_servers.tools", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("mcp_servers.tools", fmt.Errorf("invalid id: %w", err))
		}
		srv, err := deps.Servers.GetServer(ctx, id)
		if err != nil {
			return toolError("mcp_servers.tools", fmt.Errorf("get server %s: %w", id, err))
		}

		// Prefer the live connection: it yields the bare tool names and real
		// descriptions without a second handshake, and matches the shape
		// DiscoverTools returns so grants saved from either source match.
		var tools []ToolInfo
		if deps.Manager != nil {
			tools = deps.Manager.ServerToolInfos(srv.Name)
		}

		authorizationRequired := false
		if len(tools) == 0 && srv.Transport != "" {
			discovered, needsAuth, err := discoverServerTools(ctx, deps, srv)
			switch {
			case needsAuth:
				authorizationRequired = true
			case err != nil:
				return toolError("mcp_servers.tools", err)
			default:
				tools = discovered
			}
		}

		if tools == nil {
			tools = []ToolInfo{}
		}
		return jsonToolResult(map[string]any{
			"tools":                  tools,
			"authorization_required": authorizationRequired,
		})
	}
}

// discoverServerTools performs an on-demand handshake against a stored server.
// The second return value reports that the server uses OAuth and no valid token
// is available: discovery is then skipped rather than retried with the static
// headers, because those headers are not what the server actually accepts and
// listing tools the caller cannot invoke is worse than listing none.
func discoverServerTools(ctx context.Context, deps mcpServerCRUDDeps, srv *store.MCPServerData) ([]ToolInfo, bool, error) {
	var args []string
	var env, headers map[string]string
	// A malformed column is worth reporting but not worth failing on: the
	// remaining fields still describe a connectable server.
	for _, field := range []struct {
		name   string
		raw    json.RawMessage
		target any
	}{
		{"args", srv.Args, &args},
		{"env", srv.Env, &env},
		{"headers", srv.Headers, &headers},
	} {
		if len(field.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(field.raw, field.target); err != nil {
			slog.Warn("mcp_servers.tools.decode_failed", "server", srv.Name, "field", field.name, "error", err)
		}
	}

	if IsOAuthActive(srv.Settings) {
		token := ""
		if deps.OAuth != nil {
			// Discovery is server-wide, so use the global token — the tool set
			// does not vary per user even when credentials do.
			token, _ = deps.OAuth.GetValidToken(ctx, srv.ID, store.TenantIDFromContext(ctx), "")
		}
		if token == "" {
			return nil, true, nil
		}
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["Authorization"] = "Bearer " + token
	}

	tools, err := DiscoverTools(ctx, srv.Transport, srv.Command, args, env, srv.URL, headers)
	if err != nil {
		return nil, false, fmt.Errorf("discover tools for %q: %w", srv.Name, err)
	}
	return tools, false, nil
}

func handleMCPServersTest(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		transport, err := req.RequireString("transport")
		if err != nil {
			return toolError("mcp_servers.test", err)
		}
		command := req.GetString("command", "")
		url := req.GetString("url", "")
		args := stringSliceArg(req, "args")
		headers := stringMapArg(req, "headers")
		env := stringMapArg(req, "env")

		// A rejected config is a test result, not a tool failure — the caller
		// asked whether this configuration works, and the answer is no.
		if err := ValidateServerConfig(transport, command, args, url); err != nil {
			return jsonToolResult(map[string]any{"success": false, "error": err.Error()})
		}

		// For a stored OAuth server, authorization comes solely from its OAuth
		// token: a caller-supplied Authorization header would test credentials
		// the bridge will never actually use.
		if serverID := req.GetString("server_id", ""); serverID != "" && deps.Servers != nil {
			id, err := uuid.Parse(serverID)
			if err != nil {
				return toolError("mcp_servers.test", fmt.Errorf("invalid server_id: %w", err))
			}
			srv, err := deps.Servers.GetServer(ctx, id)
			if err != nil {
				return toolError("mcp_servers.test", fmt.Errorf("get server %s: %w", id, err))
			}
			if IsOAuthActive(srv.Settings) {
				delete(headers, "Authorization")
				if deps.OAuth != nil {
					token, err := deps.OAuth.GetValidToken(ctx, id, store.TenantIDFromContext(ctx), "")
					if err != nil {
						return jsonToolResult(map[string]any{"success": false, "error": "oauth token unavailable: " + err.Error()})
					}
					if token != "" {
						if headers == nil {
							headers = make(map[string]string)
						}
						headers["Authorization"] = "Bearer " + token
					}
				}
			}
		}

		tools, err := DiscoverTools(ctx, transport, command, args, env, url, headers)
		if err != nil {
			return jsonToolResult(map[string]any{"success": false, "error": err.Error()})
		}
		return jsonToolResult(map[string]any{"success": true, "tool_count": len(tools)})
	}
}

// stringMapArg reads a string→string object argument, skipping non-string
// values rather than failing the call.
func stringMapArg(req mcpgo.CallToolRequest, key string) map[string]string {
	raw, ok := req.GetArguments()[key]
	if !ok {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(obj))
	for name, value := range obj {
		if str, ok := value.(string); ok {
			out[name] = str
		}
	}
	return out
}

func handleMCPServersReconnect(deps mcpServerCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("mcp_servers.reconnect", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("mcp_servers.reconnect", fmt.Errorf("invalid id: %w", err))
		}
		srv, err := deps.Servers.GetServer(ctx, id)
		if err != nil {
			return toolError("mcp_servers.reconnect", fmt.Errorf("get server %s: %w", id, err))
		}
		if deps.Pool == nil {
			// Without a pool there is nothing cached to drop, so the next call
			// already reconnects. Say so instead of reporting a no-op as done.
			return jsonToolResult(map[string]any{"status": "no_pool", "server": srv.Name})
		}
		deps.Pool.Evict(store.TenantIDFromContext(ctx), srv.Name)
		deps.emitMCPCacheInvalidate()
		slog.Info("mcp.server.reconnect_requested", "source", "mcp_crud", "server", srv.Name, "id", id)
		return jsonToolResult(map[string]any{"status": "reconnected", "server": srv.Name})
	}
}
