package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ---------- mcp_list ----------

// MCPProxyListTool lists external MCP tools accessible to the calling agent
// by querying the MCPServerStore and connecting via the shared Pool.
type MCPProxyListTool struct {
	pool     *Pool
	mcpStore store.MCPServerStore
}

func NewMCPProxyListTool() *MCPProxyListTool { return &MCPProxyListTool{} }

func (t *MCPProxyListTool) Name() string        { return "mcp_list" }
func (t *MCPProxyListTool) Description() string  { return "List external MCP tools accessible to this agent. Returns server names and their available tools." }
func (t *MCPProxyListTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *MCPProxyListTool) Execute(ctx context.Context, _ map[string]any) *tools.Result {
	if t.pool == nil || t.mcpStore == nil {
		return tools.ErrorResult("MCP proxy not configured")
	}

	agentID := store.AgentIDFromContext(ctx)
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == store.MasterTenantID || tenantID.String() == "00000000-0000-0000-0000-000000000000" {
		tenantID = store.MasterTenantID
	}

	accessible, err := t.mcpStore.ListAccessible(ctx, agentID, "")
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to list MCP servers: %v", err))
	}
	if len(accessible) == 0 {
		return tools.NewResult("No external MCP servers are granted to this agent.")
	}

	type toolInfo struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	}
	type serverToolInfo struct {
		Server string     `json:"server"`
		Tools  []toolInfo `json:"tools"`
		Error  string     `json:"error,omitempty"`
	}

	// Connect to each server in parallel to get tool lists.
	var mu sync.Mutex
	results := make([]serverToolInfo, 0, len(accessible))
	var wg sync.WaitGroup

	for _, info := range accessible {
		info := info
		wg.Add(1)
		go func() {
			defer wg.Done()

			srv := info.Server
			rs := resolveProxyCredentials(srv)

			connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			entry, err := t.pool.Acquire(connCtx, tenantID, srv.Name, srv.Transport, srv.Command,
				rs.args, rs.env, srv.URL, rs.headers, srv.TimeoutSec)

			sti := serverToolInfo{Server: srv.Name}
			if err != nil {
				sti.Error = err.Error()
				slog.Warn("mcp_list: connect failed", "server", srv.Name, "error", err)
			} else {
				key := poolKey(tenantID, srv.Name)
				defer t.pool.Release(key)

				allowSet := toSet(info.ToolAllow)
				denySet := toSet(info.ToolDeny)

				for _, mt := range entry.MCPTools() {
					if len(denySet) > 0 {
						if _, denied := denySet[mt.Name]; denied {
							continue
						}
					}
					if len(allowSet) > 0 {
						if _, allowed := allowSet[mt.Name]; !allowed {
							continue
						}
					}
					sti.Tools = append(sti.Tools, toolInfo{
						Name:        mt.Name,
						Description: mt.Description,
						Parameters:  inputSchemaToMap(mt.InputSchema),
					})
				}
			}

			mu.Lock()
			results = append(results, sti)
			mu.Unlock()
		}()
	}
	wg.Wait()

	data, _ := json.Marshal(results)
	return tools.NewResult(string(data))
}

// ---------- mcp_call ----------

// MCPProxyCallTool calls a specific external MCP tool through the shared Pool.
type MCPProxyCallTool struct {
	pool     *Pool
	mcpStore store.MCPServerStore
}

func NewMCPProxyCallTool() *MCPProxyCallTool { return &MCPProxyCallTool{} }

func (t *MCPProxyCallTool) Name() string        { return "mcp_call" }
func (t *MCPProxyCallTool) Description() string  { return "Call an external MCP tool by server name and tool name. Use mcp_list first to discover available tools." }
func (t *MCPProxyCallTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server": map[string]any{
				"type":        "string",
				"description": "MCP server name (from mcp_list)",
			},
			"tool": map[string]any{
				"type":        "string",
				"description": "Tool name to call (from mcp_list)",
			},
			"arguments": map[string]any{
				"type":        "object",
				"description": "Arguments to pass to the tool",
			},
		},
		"required": []string{"server", "tool"},
	}
}

func (t *MCPProxyCallTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if t.pool == nil || t.mcpStore == nil {
		return tools.ErrorResult("MCP proxy not configured")
	}

	serverName, _ := args["server"].(string)
	toolName, _ := args["tool"].(string)
	toolArgs, _ := args["arguments"].(map[string]any)
	if serverName == "" || toolName == "" {
		return tools.ErrorResult("server and tool are required")
	}

	agentID := store.AgentIDFromContext(ctx)
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == store.MasterTenantID || tenantID.String() == "00000000-0000-0000-0000-000000000000" {
		tenantID = store.MasterTenantID
	}

	// Verify agent access to this server.
	accessible, err := t.mcpStore.ListAccessible(ctx, agentID, "")
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to verify access: %v", err))
	}

	var matched *store.MCPAccessInfo
	for i := range accessible {
		if accessible[i].Server.Name == serverName {
			matched = &accessible[i]
			break
		}
	}
	if matched == nil {
		return tools.ErrorResult(fmt.Sprintf("agent does not have access to MCP server %q", serverName))
	}

	// Check tool-level allow/deny.
	if len(matched.ToolDeny) > 0 {
		for _, d := range matched.ToolDeny {
			if d == toolName {
				return tools.ErrorResult(fmt.Sprintf("tool %q is denied on server %q", toolName, serverName))
			}
		}
	}
	if len(matched.ToolAllow) > 0 {
		allowed := false
		for _, a := range matched.ToolAllow {
			if a == toolName {
				allowed = true
				break
			}
		}
		if !allowed {
			return tools.ErrorResult(fmt.Sprintf("tool %q is not in the allowed list for server %q", toolName, serverName))
		}
	}

	srv := matched.Server
	rs := resolveProxyCredentials(srv)

	timeoutSec := srv.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 60
	}

	entry, err := t.pool.Acquire(ctx, tenantID, srv.Name, srv.Transport, srv.Command,
		rs.args, rs.env, srv.URL, rs.headers, srv.TimeoutSec)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to connect to MCP server %q: %v", serverName, err))
	}
	key := poolKey(tenantID, srv.Name)
	defer t.pool.Release(key)

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = toolArgs

	client := entry.ClientPtr().Load()
	if client == nil {
		return tools.ErrorResult(fmt.Sprintf("MCP server %q client not connected", serverName))
	}
	result, err := (*client).CallTool(callCtx, req)
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return tools.ErrorResult(fmt.Sprintf("MCP tool %q/%q timeout after %ds", serverName, toolName, timeoutSec))
		}
		return tools.ErrorResult(fmt.Sprintf("MCP tool %q/%q error: %v", serverName, toolName, err))
	}

	text := extractTextContent(result)
	if result.IsError {
		return tools.ErrorResult(text)
	}

	wrapped := wrapMCPContent(text, serverName, toolName)
	return tools.NewResult(wrapped)
}

// ---------- Credential resolution (mirrors Manager.resolveServerCredentials for proxy use) ----------

type proxyResolved struct {
	args    []string
	env     map[string]string
	headers map[string]string
}

func resolveProxyCredentials(srv store.MCPServerData) proxyResolved {
	args := ParseJSONBytesToStringSlice(srv.Args)
	env := ParseJSONBytesToStringMap(srv.Env)
	headers := resolveEnvVars(ParseJSONBytesToStringMap(srv.Headers))

	// Inject APIKey into Authorization header if present and not already set.
	if srv.APIKey != "" && headers["Authorization"] == "" {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["Authorization"] = "Bearer " + srv.APIKey
	}

	return proxyResolved{args: args, env: env, headers: headers}
}

// ---------- Dependency injection (called from wireExtras after Pool creation) ----------

// SetProxyToolDeps wires the Pool and MCPServerStore into the proxy tools
// that are already registered in the tool registry.
func SetProxyToolDeps(reg *tools.Registry, pool *Pool, mcpStore store.MCPServerStore) {
	if t, ok := reg.GetAny("mcp_list"); ok {
		if pt, ok := t.(*MCPProxyListTool); ok {
			pt.pool = pool
			pt.mcpStore = mcpStore
		}
	}
	if t, ok := reg.GetAny("mcp_call"); ok {
		if pt, ok := t.(*MCPProxyCallTool); ok {
			pt.pool = pool
			pt.mcpStore = mcpStore
		}
	}

	var names []string
	if pool != nil {
		names = append(names, "mcp_list", "mcp_call")
	}
	if len(names) > 0 {
		slog.Info("mcp.proxy_tools: deps wired", "tools", strings.Join(names, ","))
	}
}
