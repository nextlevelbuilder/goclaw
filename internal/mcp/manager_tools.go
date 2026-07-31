package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

// registerToolkitGroups registers one tool group per TOOLKIT within a single MCP
// server, derived from the tool names themselves.
//
// Why this exists: a tool policy entry is matched by EXACT name (see
// Registry.ExpandToolGroups) — there are no wildcards — and the only symbolic
// form is `group:<name>`. Before this, the finest grain available was
// `group:mcp:<server>`, i.e. ALL of a bridge's tools at once. For the Composio
// bridge that is every toolkit together, so "let this agent use Google Slides"
// was not expressible: a caller had to enumerate the exact tool slugs, which
// means duplicating a list that lives on the other side of the bridge and drifts
// (GOOGLESLIDES_PRESENTATIONS_CREATE, advertised by Composio's REST catalogue,
// does not exist in the raw one).
//
// Composio names its tools TOOLKIT_ACTION, so the toolkit boundary is already in
// the name and no bridge-side change is needed. Groups are named
// `mcp:<server>:<toolkit>` (lower-cased), e.g. `group:mcp:composio:googleslides`.
//
// Deliberately conservative about what counts as a prefix:
//   - the segment before the first underscore, and only if the name is entirely
//     upper-case/digits/underscores. A server using lower_snake_case tool names
//     would otherwise get a meaningless group per first word.
//   - a prefix with only ONE tool still gets a group: absence would be a silent
//     gap for callers that expect every toolkit to be addressable.
func (m *Manager) registerToolkitGroups(server string, toolNames []string) {
	byToolkit := make(map[string][]string)
	for _, full := range toolNames {
		tk, ok := toolkitPrefix(full)
		if !ok {
			continue
		}
		byToolkit[tk] = append(byToolkit[tk], full)
	}
	for tk, names := range byToolkit {
		m.registry.RegisterToolGroup("mcp:"+server+":"+tk, names)
	}
	if len(byToolkit) > 0 {
		slog.Debug("mcp.toolkit_groups.registered", "server", server, "toolkits", len(byToolkit))
	}
}

// toolkitPrefix extracts the TOOLKIT part of a TOOLKIT_ACTION tool name.
//
// The bridge may prefix tool names for namespacing, so the check runs on the
// name as registered. Returns false for anything that is not a SHOUTING_SNAKE
// name with at least one underscore, so non-Composio servers are left alone.
func toolkitPrefix(name string) (string, bool) {
	i := strings.IndexByte(name, '_')
	if i <= 0 || i == len(name)-1 {
		return "", false
	}
	for j := 0; j < len(name); j++ {
		c := name[j]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return "", false
	}
	return strings.ToLower(name[:i]), true
}

// unregisterToolkitGroups drops the per-toolkit groups for one server, so a
// disconnected bridge does not leave grants pointing at groups that no longer
// resolve.
func (m *Manager) unregisterToolkitGroups(server string, toolNames []string) {
	seen := make(map[string]bool)
	for _, full := range toolNames {
		if tk, ok := toolkitPrefix(full); ok && !seen[tk] {
			seen[tk] = true
			m.registry.UnregisterToolGroup("mcp:" + server + ":" + tk)
		}
	}
}

// toolNamesForServer returns the tool names registered for one server, whether it
// is pool-backed or standalone. Caller must hold m.mu.
func (m *Manager) toolNamesForServer(name string) []string {
	if names, ok := m.poolToolNames[name]; ok {
		return names
	}
	if ss := m.servers[name]; ss != nil {
		return ss.toolNames
	}
	return nil
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
		// Drop the per-toolkit groups too. Leaving them behind would let a policy
		// entry keep resolving to tools that are no longer registered.
		if names := m.toolNamesForServer(name); len(names) > 0 {
			m.unregisterToolkitGroups(name, names)
		}
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
