package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UserCredServers returns servers requiring per-user credentials.
// These are stored during LoadForAgent("") and used by the agent loop
// for per-request tool resolution via pool.AcquireUser().
func (m *Manager) UserCredServers() []store.MCPAccessInfo {
	return m.userCredServers
}

// ToolNames returns all registered MCP tool names.
func (m *Manager) ToolNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for name, ss := range m.servers {
		if _, isPool := m.poolServers[name]; isPool {
			names = append(names, m.poolToolNames[name]...)
		} else {
			names = append(names, ss.toolNames...)
		}
	}
	return names
}

// ServerToolNames returns tool names for a specific server.
func (m *Manager) ServerToolNames(serverName string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, isPool := m.poolServers[serverName]; isPool {
		return append([]string(nil), m.poolToolNames[serverName]...)
	}
	if ss, ok := m.servers[serverName]; ok {
		return append([]string(nil), ss.toolNames...)
	}
	return nil
}

// ServerToolInfos returns ToolInfo (original/bare tool name + real description)
// for a server that is already live-connected in this Manager, by looking up
// each registered (prefixed) tool name in the registry and reading its
// original MCP name and description off the live *BridgeTool.
//
// This exists so callers needing tool metadata for an already-connected
// server (e.g. handleListServerTools) get the SAME bare-name shape as the
// on-demand DiscoverTools fallback (manager_tools.go DiscoverTools), instead
// of the registered/prefixed names returned by ServerToolNames. Mixing the
// two shapes previously caused a server's tool_allow grant (built from
// whichever name shape the UI happened to see, depending on whether the
// server was already connected) to end up keyed by a prefixed name that
// never matches the bare-name keys used by buildCachedToolInfo
// (manager_connect.go) and ListToolsForAgent's tool_cache lookups
// (manager.go), silently producing empty description/parameters in the
// prompt preview for tools whose grant was captured while the server was
// live-connected — most commonly a server (like goclaw's own CRUD server)
// that self-connects and therefore tends to already be live by the time an
// operator configures its grants.
func (m *Manager) ServerToolInfos(serverName string) []ToolInfo {
	m.mu.RLock()
	var registeredNames []string
	if _, isPool := m.poolServers[serverName]; isPool {
		registeredNames = m.poolToolNames[serverName]
	} else if ss, ok := m.servers[serverName]; ok {
		registeredNames = ss.toolNames
	}
	m.mu.RUnlock()

	if len(registeredNames) == 0 {
		return nil
	}

	infos := make([]ToolInfo, 0, len(registeredNames))
	for _, registeredName := range registeredNames {
		tool, ok := m.registry.Get(registeredName)
		if !ok {
			continue
		}
		bridgeTool, ok := tool.(*BridgeTool)
		if !ok {
			continue
		}
		infos = append(infos, ToolInfo{
			Name:        bridgeTool.OriginalName(),
			Description: bridgeTool.Description(),
		})
	}
	return infos
}

// updateMCPGroup rebuilds the "mcp" group with all MCP tool names across servers.
// Must be called with m.mu NOT held (it acquires RLock).
func (m *Manager) updateMCPGroup() {
	allNames := m.ToolNames()
	if len(allNames) > 0 {
		m.registry.RegisterToolGroup("mcp", allNames)
	} else {
		m.registry.UnregisterToolGroup("mcp")
	}
}

// unregisterAllTools removes all MCP tools from the registry.
func (m *Manager) unregisterAllTools() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range m.servers {
		if _, isPool := m.poolServers[name]; isPool {
			// Pool-backed: unregister per-agent tools, release shared connection
			for _, toolName := range m.poolToolNames[name] {
				m.registry.Unregister(toolName)
			}
			if m.pool != nil {
				if pkey, ok := m.poolKeys[name]; ok {
					m.pool.Release(pkey)
				}
			}
		} else {
			// Standalone: close connection directly
			ss := m.servers[name]
			if ss.cancel != nil {
				ss.cancel()
			}
			if ss.client != nil {
				_ = ss.client.Close()
			}
			for _, toolName := range ss.toolNames {
				m.registry.Unregister(toolName)
			}
		}
		m.registry.UnregisterToolGroup("mcp:" + name)
		slog.Debug("mcp.server.unregistered", "server", name)
	}

	// Clean up search mode state: unregister activated tools and clear deferred
	if m.searchMode {
		for name := range m.activatedTools {
			m.registry.Unregister(name)
		}
		m.deferredTools = nil
		m.activatedTools = nil
		m.searchMode = false
	}

	m.servers = make(map[string]*serverState)
	m.poolServers = nil
	m.poolToolNames = nil
	m.registry.UnregisterToolGroup("mcp")
}

// ToolInfo holds a tool's name and description for API responses.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// DiscoverTools connects temporarily to an MCP server, lists its tools, and disconnects.
// Used for on-demand discovery when no persistent Manager connection exists (DB-backed servers).
func DiscoverTools(ctx context.Context, transportType, command string, args []string, env map[string]string, url string, headers map[string]string) ([]ToolInfo, error) {
	mcpTools, err := discoverRawTools(ctx, transportType, command, args, env, url, headers, discoverToolsTimeout)
	if err != nil {
		return nil, err
	}

	result := make([]ToolInfo, 0, len(mcpTools))
	for _, t := range mcpTools {
		result = append(result, ToolInfo{Name: t.Name, Description: t.Description})
	}
	return result, nil
}

// discoverToolsTimeout bounds a full DiscoverTools/discoverRawTools round trip
// (connect + initialize + list tools) against a remote MCP server.
const discoverToolsTimeout = 15 * time.Second

// discoverRawTools connects temporarily to an MCP server, lists its tools with
// their full mcpgo.Tool definitions (including input schema), and disconnects.
// Shared by DiscoverTools (name+description only, for the admin browse UI) and
// the ListToolsForAgent on-demand fallback (full schema, for prompt preview
// caching), so both callers see identical live tool data.
func discoverRawTools(ctx context.Context, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, timeout time.Duration) ([]mcpgo.Tool, error) {
	if err := ValidateServerConfig(transportType, command, args, url); err != nil {
		return nil, fmt.Errorf("invalid MCP server config: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := createClient(transportType, command, args, env, url, headers)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	defer client.Close()

	if transportType != "stdio" {
		if err := client.Start(ctx); err != nil {
			return nil, fmt.Errorf("start transport: %w", err)
		}
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "goclaw-discovery", Version: "1.0.0"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return toolsResult.Tools, nil
}
