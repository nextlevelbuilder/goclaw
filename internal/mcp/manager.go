package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"

	"github.com/google/uuid"
)

const (
	healthCheckInterval  = 30 * time.Second
	initialBackoff       = 2 * time.Second
	maxBackoff           = 60 * time.Second
	maxReconnectAttempts = 10

	// mcpToolInlineMaxCount is the threshold above which MCP tools switch
	// to search mode (deferred loading via mcp_tool_search) instead of
	// being registered inline in the tool registry.
	mcpToolInlineMaxCount = 40
)

// ServerStatus reports the connection status of an MCP server.
type ServerStatus struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
	Error     string `json:"error,omitempty"`
}

// serverState tracks a single MCP server connection.
type serverState struct {
	name       string
	transport  string
	client     *mcpclient.Client
	connected  atomic.Bool
	toolNames  []string // registered tool names in the registry
	timeoutSec int
	cancel     context.CancelFunc

	mu              sync.Mutex
	reconnAttempts  int
	lastErr         string
}

// Manager orchestrates MCP server connections and tool registration.
// Supports two sources:
//   - Config-based: reads from config.MCPServerConfig map (shared across all agents)
//   - DB-backed: queries MCPServerStore per agent+user for permission-filtered servers
//
// When total MCP tool count exceeds mcpToolInlineMaxCount, the manager
// enters "search mode": tools are kept in deferredTools instead of the
// registry, and only mcp_tool_search is registered. Tools are activated
// on demand via ActivateTools().
type Manager struct {
	mu       sync.RWMutex
	servers  map[string]*serverState
	registry *tools.Registry

	// Config-based servers
	configs map[string]*config.MCPServerConfig

	// DB-backed servers
	store store.MCPServerStore

	// Shared connection pool (nil = standalone mode)
	pool          *Pool
	poolServers   map[string]struct{}  // server names acquired from pool (for cleanup)
	poolToolNames map[string][]string  // per-agent tool names for pool-backed servers

	// Search mode: deferred tools not registered in registry
	deferredTools  map[string]*BridgeTool // registeredName → BridgeTool
	activatedTools map[string]struct{}     // tracks activated tool names for group:mcp
	searchMode     bool
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithConfigs sets static MCP server configs from the config file.
func WithConfigs(cfgs map[string]*config.MCPServerConfig) ManagerOption {
	return func(m *Manager) {
		m.configs = cfgs
	}
}

// WithStore sets the MCPServerStore for DB-backed MCP server loading.
func WithStore(s store.MCPServerStore) ManagerOption {
	return func(m *Manager) {
		m.store = s
	}
}

// WithPool sets a shared connection pool for MCP servers.
// When set, LoadForAgent uses the pool instead of creating per-agent connections.
func WithPool(p *Pool) ManagerOption {
	return func(m *Manager) {
		m.pool = p
	}
}

// NewManager creates a new MCP Manager.
func NewManager(registry *tools.Registry, opts ...ManagerOption) *Manager {
	m := &Manager{
		servers:  make(map[string]*serverState),
		registry: registry,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start connects to all config-file MCP servers.
// Non-fatal: logs warnings for servers that fail to connect and continues.
func (m *Manager) Start(ctx context.Context) error {
	if len(m.configs) == 0 {
		return nil
	}

	var errs []string
	for name, cfg := range m.configs {
		if !cfg.IsEnabled() {
			slog.Info("mcp.server.disabled", "server", name)
			continue
		}

		if err := m.connectServer(ctx, name, cfg.Transport, cfg.Command, cfg.Args, cfg.Env, cfg.URL, cfg.Headers, cfg.ToolPrefix, cfg.TimeoutSec); err != nil {
			slog.Warn("mcp.server.connect_failed", "server", name, "error", err)
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some MCP servers failed to connect: %s", joinErrors(errs))
	}
	return nil
}

// LoadForAgent connects MCP servers accessible by a specific agent+user.
// Previously registered MCP tools for this manager are cleared and reloaded.
func (m *Manager) LoadForAgent(ctx context.Context, agentID uuid.UUID, userID string) error {
	if m.store == nil {
		return nil
	}

	accessible, err := m.store.ListAccessible(ctx, agentID, userID)
	if err != nil {
		return fmt.Errorf("list accessible MCP servers: %w", err)
	}

	// Unregister all existing MCP tools first
	m.unregisterAllTools()

	for _, info := range accessible {
		srv := info.Server
		if !srv.Enabled {
			continue
		}

		args := jsonBytesToStringSlice(srv.Args)
		env := jsonBytesToStringMap(srv.Env)
		headers := jsonBytesToStringMap(srv.Headers)

		if m.pool != nil {
			// Pool mode: acquire shared connection, create per-agent BridgeTools
			if err := m.connectViaPool(ctx, srv.Name, srv.Transport, srv.Command,
				args, env, srv.URL, headers, srv.ToolPrefix, srv.TimeoutSec); err != nil {
				slog.Warn("mcp.server.connect_failed", "server", srv.Name, "error", err)
				continue
			}
		} else {
			// Standalone mode: create per-agent connection
			if err := m.connectServer(ctx, srv.Name, srv.Transport, srv.Command,
				args, env, srv.URL, headers,
				srv.ToolPrefix, srv.TimeoutSec); err != nil {
				slog.Warn("mcp.server.connect_failed", "server", srv.Name, "error", err)
				continue
			}
		}

		// Apply tool filtering from grants
		if len(info.ToolAllow) > 0 || len(info.ToolDeny) > 0 {
			m.filterTools(srv.Name, info.ToolAllow, info.ToolDeny)
		}
	}

	// Check if we should enter search mode (too many tools to inline)
	m.maybeEnterSearchMode()

	return nil
}

// maybeEnterSearchMode moves all registered BridgeTools to deferredTools
// if total count exceeds the inline threshold.
func (m *Manager) maybeEnterSearchMode() {
	allNames := m.ToolNames()
	if len(allNames) <= mcpToolInlineMaxCount {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.deferredTools = make(map[string]*BridgeTool, len(allNames))
	m.activatedTools = make(map[string]struct{})

	// Move all tools to deferred — handle both pool-backed and standalone
	for serverName := range m.servers {
		var toolNames []string
		if _, isPool := m.poolServers[serverName]; isPool {
			toolNames = m.poolToolNames[serverName]
		} else {
			toolNames = m.servers[serverName].toolNames
		}

		for _, name := range toolNames {
			if bt, ok := m.registry.Get(name); ok {
				if bridge, ok := bt.(*BridgeTool); ok {
					m.deferredTools[name] = bridge
					m.registry.Unregister(name)
				}
			}
		}

		// Clear tool names
		if _, isPool := m.poolServers[serverName]; isPool {
			m.poolToolNames[serverName] = nil
		} else {
			m.servers[serverName].toolNames = nil
		}
	}

	tools.UnregisterToolGroup("mcp")
	m.searchMode = true

	slog.Info("mcp.search_mode.enabled",
		"deferred_tools", len(m.deferredTools),
		"threshold", mcpToolInlineMaxCount)
}

// IsSearchMode reports whether the manager is in deferred/search mode.
func (m *Manager) IsSearchMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.searchMode
}

// DeferredToolInfos returns all deferred tools for BM25 indexing.
func (m *Manager) DeferredToolInfos() []*BridgeTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*BridgeTool, 0, len(m.deferredTools))
	for _, bt := range m.deferredTools {
		result = append(result, bt)
	}
	return result
}

// ActivateTools moves named deferred tools into the registry so
// they become available on the next agent loop iteration.
// Uses 3-phase locking to avoid deadlock with registry.mu.
func (m *Manager) ActivateTools(names []string) {
	// Phase 1: collect tools to activate (read lock)
	m.mu.RLock()
	toActivate := make([]*BridgeTool, 0, len(names))
	for _, name := range names {
		if bt, ok := m.deferredTools[name]; ok {
			if _, exists := m.registry.Get(name); !exists {
				toActivate = append(toActivate, bt)
			}
		}
	}
	m.mu.RUnlock()

	if len(toActivate) == 0 {
		return
	}

	// Phase 2: register in registry (no Manager lock held)
	var activated []string
	for _, bt := range toActivate {
		if _, exists := m.registry.Get(bt.Name()); !exists {
			m.registry.Register(bt)
			activated = append(activated, bt.Name())
		}
	}

	if len(activated) == 0 {
		return
	}

	// Phase 3: update internal state (write lock)
	m.mu.Lock()
	for _, name := range activated {
		delete(m.deferredTools, name)
		m.activatedTools[name] = struct{}{}
	}
	activeNames := make([]string, 0, len(m.activatedTools))
	for n := range m.activatedTools {
		activeNames = append(activeNames, n)
	}
	m.mu.Unlock()

	tools.RegisterToolGroup("mcp", activeNames)
	slog.Info("mcp.tools.activated", "tools", activated)
}

// Stop shuts down all MCP server connections and unregisters tools.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, ss := range m.servers {
		if _, isPool := m.poolServers[name]; isPool {
			// Pool-backed: unregister per-agent tools, release shared connection
			for _, toolName := range m.poolToolNames[name] {
				m.registry.Unregister(toolName)
			}
			if m.pool != nil {
				m.pool.Release(name)
			}
		} else {
			// Standalone: close connection directly
			if ss.cancel != nil {
				ss.cancel()
			}
			if ss.client != nil {
				if err := ss.client.Close(); err != nil {
					slog.Debug("mcp.server.close_error", "server", name, "error", err)
				}
			}
			for _, toolName := range ss.toolNames {
				m.registry.Unregister(toolName)
			}
		}
	}
	m.servers = make(map[string]*serverState)
	m.poolServers = nil
	m.poolToolNames = nil
}

// ServerStatus returns the status of all connected MCP servers.
func (m *Manager) ServerStatus() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]ServerStatus, 0, len(m.servers))
	for _, ss := range m.servers {
		statuses = append(statuses, ServerStatus{
			Name:      ss.name,
			Transport: ss.transport,
			Connected: ss.connected.Load(),
			ToolCount: len(ss.toolNames),
			Error:     ss.lastErr,
		})
	}
	return statuses
}
