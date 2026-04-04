package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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

// updateMCPGroup rebuilds the "mcp" group with all MCP tool names across servers.
// Must be called with m.mu NOT held (it acquires RLock).
func (m *Manager) updateMCPGroup() {
	allNames := m.ToolNames()
	if len(allNames) > 0 {
		tools.RegisterToolGroup("mcp", allNames)
	} else {
		tools.UnregisterToolGroup("mcp")
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
		tools.UnregisterToolGroup("mcp:" + name)
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
	tools.UnregisterToolGroup("mcp")
}

// ToolInfo holds a tool's name and description for API responses.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// DiscoverTools connects temporarily to an MCP server, lists its tools, and disconnects.
// Used for on-demand discovery when no persistent Manager connection exists (DB-backed servers).
func DiscoverTools(ctx context.Context, transportType, command string, args []string, env map[string]string, url string, headers map[string]string) ([]ToolInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
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

	result := make([]ToolInfo, 0, len(toolsResult.Tools))
	for _, t := range toolsResult.Tools {
		result = append(result, ToolInfo{Name: t.Name, Description: t.Description})
	}
	return result, nil
}

// filterTools removes tools from the registry that don't match the allow/deny lists.
func (m *Manager) filterTools(serverName string, allow, deny []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get the tool names list (pool-backed or standalone)
	var toolNames []string
	_, isPool := m.poolServers[serverName]
	if isPool {
		toolNames = m.poolToolNames[serverName]
	} else if ss, ok := m.servers[serverName]; ok {
		toolNames = ss.toolNames
	} else {
		return
	}

	allowSet := toSet(allow)
	denySet := toSet(deny)

	var kept []string
	for _, toolName := range toolNames {
		bt, ok := m.registry.Get(toolName)
		if !ok {
			continue
		}
		bridge, ok := bt.(*BridgeTool)
		if !ok {
			kept = append(kept, toolName)
			continue
		}
		origName := bridge.OriginalName()

		// Deny takes priority
		if _, denied := denySet[origName]; denied {
			m.registry.Unregister(toolName)
			continue
		}

		// If allow list is set, only keep tools in the allow list
		if len(allowSet) > 0 {
			if _, allowed := allowSet[origName]; !allowed {
				m.registry.Unregister(toolName)
				continue
			}
		}

		kept = append(kept, toolName)
	}

	// Update the correct tool names list
	if isPool {
		m.poolToolNames[serverName] = kept
	} else {
		m.servers[serverName].toolNames = kept
	}
}
