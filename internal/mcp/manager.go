package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	healthFailThreshold  = 3 // consecutive ping failures before marking disconnected
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
	healthFailures  int // consecutive ping failures (resets on success)
	lastErr         string
}

// Manager orchestrates MCP server connections and tool registration.
// Supports two sources:
//   - Config-based: reads from config.MCPServerConfig map (shared across all agents)
//   - DB-backed: queries MCPServerStore per agent+user for permission-filtered servers
//
// When total MCP tool count exceeds mcpToolInlineMaxCount, the manager
// enters hybrid search mode: the first mcpToolInlineMaxCount tools stay
// registered inline, while excess tools move to deferredTools and are
// discovered via mcp_tool_search. Tools are activated on demand via
// ActivateTools().
type Manager struct {
	mu       sync.RWMutex
	servers  map[string]*serverState
	registry *tools.Registry

	// Config-based servers
	configs map[string]*config.MCPServerConfig

	// DB-backed servers
	store store.MCPServerStore

	// Shared connection pool (nil = config-only mode)
	pool          *Pool
	poolServers   map[string]struct{}  // server names acquired from pool (for cleanup)
	poolToolNames map[string][]string  // per-agent tool names for pool-backed servers
	poolKeys      map[string]string   // server name → pool compound key (tenantID/name) for Release

	// Search mode: deferred tools not registered in registry
	deferredTools  map[string]*BridgeTool // registeredName → BridgeTool
	activatedTools map[string]struct{}     // tracks activated tool names for group:mcp
	searchMode     bool

	// User-credential servers: servers requiring per-user credentials, stored during
	// LoadForAgent("") for later per-request tool resolution. These servers are NOT
	// connected at startup — connections are created per-user via pool.AcquireUser().
	userCredServers []store.MCPAccessInfo
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

		if err := m.connectServer(ctx, name, cfg.Transport, cfg.Command, cfg.Args, cfg.Env, cfg.URL, resolveEnvVars(cfg.Headers), cfg.ToolPrefix, cfg.TimeoutSec); err != nil {
			slog.Warn("mcp.server.connect_failed", "server", name, "error", err)
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some MCP servers failed to connect: %s", joinErrors(errs))
	}
	return nil
}

// resolvedServer holds a server config with merged credentials ready for connection.
type resolvedServer struct {
	info         store.MCPAccessInfo
	args         []string
	env          map[string]string
	headers      map[string]string
	hasUserCreds bool
}

// resolveServerCredentials merges server defaults with per-user credentials.
// Returns nil if the server should be skipped (disabled or missing required creds).
func (m *Manager) resolveServerCredentials(ctx context.Context, info store.MCPAccessInfo, userID string) *resolvedServer {
	srv := info.Server
	if !srv.Enabled {
		return nil
	}

	// Skip server if it requires per-user credentials and user has none
	if requireUserCreds(srv.Settings) {
		if userID == "" {
			return nil
		}
		uc, _ := m.store.GetUserCredentials(ctx, srv.ID, userID)
		if uc == nil || (uc.APIKey == "" && len(uc.Headers) == 0 && len(uc.Env) == 0) {
			slog.Debug("mcp.skip_no_user_credentials", "server", srv.Name, "user", userID)
			return nil
		}
	}

	args := jsonBytesToStringSlice(srv.Args)
	env := jsonBytesToStringMap(srv.Env)
	headers := resolveEnvVars(jsonBytesToStringMap(srv.Headers))

	// Inject APIKey into headers if present (bug fix: was never passed to connections)
	if srv.APIKey != "" && headers["Authorization"] == "" {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["Authorization"] = "Bearer " + srv.APIKey
	}

	// Merge per-user credentials (user overrides server defaults)
	if userID != "" && m.store != nil {
		if userCreds, err := m.store.GetUserCredentials(ctx, srv.ID, userID); err == nil && userCreds != nil {
			if userCreds.APIKey != "" {
				if headers == nil {
					headers = make(map[string]string)
				}
				headers["Authorization"] = "Bearer " + userCreds.APIKey
			}
			for k, v := range userCreds.Headers {
				if headers == nil {
					headers = make(map[string]string)
				}
				headers[k] = v
			}
			for k, v := range userCreds.Env {
				if env == nil {
					env = make(map[string]string)
				}
				env[k] = v
			}
		}
	}

	// Per-user credentials change connection params → can't share pool connection.
	// Fall back to per-agent mode when user has custom credentials.
	hasUserCreds := userID != "" && m.store != nil
	if hasUserCreds {
		if uc, _ := m.store.GetUserCredentials(ctx, srv.ID, userID); uc != nil && (uc.APIKey != "" || len(uc.Headers) > 0 || len(uc.Env) > 0) {
			hasUserCreds = true
		} else {
			hasUserCreds = false
		}
	}

	return &resolvedServer{
		info:         info,
		args:         args,
		env:          env,
		headers:      headers,
		hasUserCreds: hasUserCreds,
	}
}

// connectAndFilter establishes the MCP connection (pool or per-agent mode)
// and applies tool allow/deny filtering from server grants.
func (m *Manager) connectAndFilter(ctx context.Context, rs *resolvedServer) error {
	srv := rs.info.Server

	if m.pool != nil && !rs.hasUserCreds {
		// Pool mode: acquire shared connection, create per-agent BridgeTools
		tid := store.TenantIDFromContext(ctx)
		if err := m.connectViaPool(ctx, tid, srv.Name, srv.Transport, srv.Command,
			rs.args, rs.env, srv.URL, rs.headers, srv.ToolPrefix, srv.TimeoutSec); err != nil {
			return err
		}
	} else {
		// Per-agent mode: create per-agent connection
		if err := m.connectServer(ctx, srv.Name, srv.Transport, srv.Command,
			rs.args, rs.env, srv.URL, rs.headers,
			srv.ToolPrefix, srv.TimeoutSec); err != nil {
			return err
		}
	}

	// Apply tool filtering from grants
	if len(rs.info.ToolAllow) > 0 || len(rs.info.ToolDeny) > 0 {
		m.filterTools(srv.Name, rs.info.ToolAllow, rs.info.ToolDeny)
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
	m.userCredServers = nil

	for _, info := range accessible {
		// When loading at startup (userID=""), store servers requiring per-user
		// credentials for later per-request resolution instead of skipping them.
		if userID == "" && requireUserCreds(info.Server.Settings) && info.Server.Enabled {
			m.userCredServers = append(m.userCredServers, info)
			slog.Debug("mcp.server.deferred_user_creds", "server", info.Server.Name)
			continue
		}

		rs := m.resolveServerCredentials(ctx, info, userID)
		if rs == nil {
			continue
		}
		if err := m.connectAndFilter(ctx, rs); err != nil {
			slog.Warn("mcp.server.connect_failed", "server", info.Server.Name, "error", err)
		}
	}

	// Check if we should enter search mode (too many tools to inline)
	m.maybeEnterSearchMode()

	return nil
}

// maybeEnterSearchMode partially defers MCP tools when total count exceeds
// the inline threshold. The first mcpToolInlineMaxCount tools stay registered
// inline; the rest are moved to deferredTools and discovered via mcp_tool_search.
func (m *Manager) maybeEnterSearchMode() {
	allNames := m.ToolNames()
	if len(allNames) <= mcpToolInlineMaxCount {
		return
	}

	// Build a set of names to defer (everything beyond the threshold).
	deferSet := make(map[string]struct{}, len(allNames)-mcpToolInlineMaxCount)
	for _, name := range allNames[mcpToolInlineMaxCount:] {
		deferSet[name] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.deferredTools = make(map[string]*BridgeTool, len(deferSet))
	m.activatedTools = make(map[string]struct{})

	// Move only excess tools to deferred, keep the rest inline.
	for serverName := range m.servers {
		var toolNames []string
		if _, isPool := m.poolServers[serverName]; isPool {
			toolNames = m.poolToolNames[serverName]
		} else {
			toolNames = m.servers[serverName].toolNames
		}

		var kept []string
		for _, name := range toolNames {
			if _, shouldDefer := deferSet[name]; !shouldDefer {
				kept = append(kept, name)
				continue
			}
			if bt, ok := m.registry.Get(name); ok {
				if bridge, ok := bt.(*BridgeTool); ok {
					m.deferredTools[name] = bridge
					m.registry.Unregister(name)
				}
			}
		}

		// Update per-server tool names to only the kept inline tools.
		if _, isPool := m.poolServers[serverName]; isPool {
			m.poolToolNames[serverName] = kept
		} else {
			m.servers[serverName].toolNames = kept
		}
	}

	// Update "mcp" group to only the kept inline names.
	inlineNames := allNames[:mcpToolInlineMaxCount]
	tools.RegisterToolGroup("mcp", inlineNames)
	m.searchMode = true

	slog.Info("mcp.search_mode.enabled",
		"inline_tools", len(inlineNames),
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

// ActivateToolIfDeferred activates a single named tool if it is currently deferred.
// Returns true if the tool is now in the registry.
// Used by the Registry's deferredActivator callback for lazy tool activation.
func (m *Manager) ActivateToolIfDeferred(name string) bool {
	m.mu.Lock()
	_, isDeferred := m.deferredTools[name]
	_, isActivated := m.activatedTools[name]
	if isActivated {
		m.mu.Unlock()
		return true // already activated by a concurrent call
	}
	if !isDeferred {
		m.mu.Unlock()
		return false
	}
	// Mark as activated under lock to prevent concurrent ActivateTools races.
	m.activatedTools[name] = struct{}{}
	bt := m.deferredTools[name]
	delete(m.deferredTools, name)
	activeNames := make([]string, 0, len(m.activatedTools))
	for n := range m.activatedTools {
		activeNames = append(activeNames, n)
	}
	m.mu.Unlock()

	// Register in registry outside lock (registry has its own sync).
	m.registry.Register(bt)
	tools.RegisterToolGroup("mcp", activeNames)
	slog.Info("mcp.tools.activated", "tools", []string{name})
	return true
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
				if pkey, ok := m.poolKeys[name]; ok {
					m.pool.Release(pkey)
				}
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

// resolveEnvVars returns a copy of m with "env:VARNAME" values resolved to os.Getenv("VARNAME").
func resolveEnvVars(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if after, ok := strings.CutPrefix(v, "env:"); ok {
			out[k] = os.Getenv(after)
		} else {
			out[k] = v
		}
	}
	return out
}

// requireUserCreds checks if an MCP server's settings mandate per-user credentials.
func requireUserCreds(settings json.RawMessage) bool {
	if len(settings) == 0 {
		return false
	}
	var s struct {
		RequireUserCredentials bool `json:"require_user_credentials"`
	}
	_ = json.Unmarshal(settings, &s)
	return s.RequireUserCredentials
}
