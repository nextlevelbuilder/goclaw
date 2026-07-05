package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	reconnectCooldown    = 5 * time.Minute // wait after exhausting reconnect attempts before retrying

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

// connParams stores connection parameters needed to re-establish a dead connection.
// Populated during initial connectAndDiscover and used by tryReconnect.
type connParams struct {
	command string
	args    []string
	env     map[string]string
	url     string
	headers map[string]string
}

// serverState tracks a single MCP server connection.
//
// Dual-pointer design for the MCP client:
//   - client: direct pointer used by healthLoop (single goroutine, no contention).
//   - clientPtr: atomic pointer shared with all BridgeTools via NewBridgeTool.
//     BridgeTools call clientPtr.Load() in Execute for race-safe access.
//
// On reconnect, fullReconnect() updates BOTH: ss.client for healthLoop and
// ss.clientPtr.Store() for BridgeTools. The old client is closed AFTER the swap.
type serverState struct {
	name       string
	transport  string
	client     *mcpclient.Client                // direct ref for health checks (single-goroutine access)
	clientPtr  atomic.Pointer[mcpclient.Client] // shared atomic ref for BridgeTools (multi-goroutine safe)
	connected  atomic.Bool
	toolNames  []string // registered tool names in the registry
	timeoutSec int
	cancel     context.CancelFunc
	conn       connParams // connection params for reconnect

	mu             sync.Mutex
	reconnAttempts int
	healthFailures int // consecutive ping failures (resets on success)
	lastErr        string

	// reconnPending is set when a BridgeTool detects the server reset its
	// session lifecycle (FastMCP-style "tools/call invalid during session
	// initialization" error) and force-reconnect is in flight. The health
	// loop must skip its ping while pending, otherwise a server that still
	// answers `ping` in "initializing" state would clobber connected=true
	// before the fresh Initialize completes — leaving the pool to keep
	// serving the dead session.
	reconnPending atomic.Bool
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

	// Grant checker for runtime grant verification (nil = skip check)
	grantChecker GrantChecker

	// Shared connection pool (nil = config-only mode)
	pool          *Pool
	poolServers   map[string]struct{} // server names acquired from pool (for cleanup)
	poolToolNames map[string][]string // per-agent tool names for pool-backed servers
	poolKeys      map[string]string   // server name → pool compound key (tenantID/name) for Release

	// Search mode: deferred tools not registered in registry
	deferredTools  map[string]*BridgeTool // registeredName → BridgeTool
	activatedTools map[string]struct{}    // tracks activated tool names for group:mcp
	searchMode     bool

	// User-credential servers: servers requiring per-user credentials, stored during
	// LoadForAgent("") for later per-request tool resolution. These servers are NOT
	// connected at startup — connections are created per-user via pool.AcquireUser().
	userCredServers []store.MCPAccessInfo

	// oauthTokenProvider resolves a live Bearer token for OAuth-enabled MCP servers.
	// nil = OAuth token injection disabled.
	oauthTokenProvider OAuthTokenProvider
}

// OAuthTokenProvider retrieves a valid OAuth Bearer token for an MCP server.
// userID="" means global token; non-empty means per-user token.
type OAuthTokenProvider interface {
	GetValidToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (string, error)
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithConfigs sets static MCP server configs from the config file.
func WithConfigs(cfgs map[string]*config.MCPServerConfig) ManagerOption {
	return func(m *Manager) {
		m.configs = cfgs
	}
}

// SetConfigs replaces the static server config map on an already-constructed Manager.
// This is used by the gateway to populate configs from the database after the store
// is initialised, before calling Start.
func (m *Manager) SetConfigs(cfgs map[string]*config.MCPServerConfig) {
	slog.Debug("mcp.Manager.SetConfigs: applying configs", "count", len(cfgs))
	m.configs = cfgs
	slog.Debug("mcp.Manager.SetConfigs: configs applied successfully", "count", len(cfgs))
}

// WithStore sets the MCPServerStore for DB-backed MCP server loading.
func WithStore(s store.MCPServerStore) ManagerOption {
	return func(m *Manager) {
		m.store = s
	}
}

// SetStore sets the MCP store on an already-constructed Manager.
// Use this when the store is not yet available at construction time.
func (m *Manager) SetStore(s store.MCPServerStore) {
	m.store = s
}

// WithPool sets a shared connection pool for MCP servers.
// When set, LoadForAgent uses the pool instead of creating per-agent connections.
func WithPool(p *Pool) ManagerOption {
	return func(m *Manager) {
		m.pool = p
	}
}

// WithGrantChecker sets the grant checker for runtime grant verification.
// When set, BridgeTool.Execute rechecks grants before executing tools.
func WithGrantChecker(gc GrantChecker) ManagerOption {
	return func(m *Manager) {
		m.grantChecker = gc
	}
}

// WithOAuthTokenProvider sets the OAuth token provider for Bearer token injection.
func WithOAuthTokenProvider(p OAuthTokenProvider) ManagerOption {
	return func(m *Manager) {
		m.oauthTokenProvider = p
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
	slog.Debug("mcp.Manager.Start: called", "configs", len(m.configs))
	if len(m.configs) == 0 {
		slog.Debug("mcp.Manager.Start: no configs, returning early")
		return nil
	}

	var errs []string
	for name, cfg := range m.configs {
		if !cfg.IsEnabled() {
			slog.Debug("mcp.server.disabled", "server", name)
			continue
		}

		slog.Debug("mcp.Manager.Start: starting server", "name", name, "transport", cfg.Transport)
		// Config-path servers have no DB ID — pass uuid.Nil
		headers, err := resolveEnvVars(cfg.Headers)
		if err != nil {
			slog.Warn("security.mcp.env_var_rejected", "server", name, "err", err)
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		// Config-path servers have no DB-backed Settings, so no tool hints.
		// Also no grant-based tool filtering (allow/deny are nil) — config
		// trust model is that the operator gates which servers are enabled.
		if err := m.connectServer(ctx, name, cfg.Transport, cfg.Command, cfg.Args, cfg.Env, cfg.URL, headers, cfg.ToolPrefix, cfg.TimeoutSec, uuid.Nil, ToolHints{}, nil, nil); err != nil {
			slog.Warn("mcp.server.connect_failed", "server", name, "error", err)
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		} else {
			m.mu.RLock()
			toolCount := 0
			if ss, ok := m.servers[name]; ok {
				toolCount = len(ss.toolNames)
			}
			m.mu.RUnlock()
			slog.Debug("mcp.Manager.Start: server started", "name", name, "tools", toolCount)
		}
	}

	totalTools := len(m.ToolNames())
	slog.Debug("mcp.Manager.Start: fully started", "total_tools", totalTools, "errors", len(errs))
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

	var contextCreds *store.MCPContextCredentials
	if contextStore, ok := m.store.(store.MCPContextAdminStore); ok {
		for _, scope := range store.ChannelContextScopeChainFromContext(ctx) {
			if creds, _ := contextStore.GetContextCredentialsForScope(ctx, scope, srv.ID); creds != nil {
				contextCreds = creds
			}
		}
	}

	// Skip server if it requires scoped/user credentials and none are present.
	if requireUserCreds(srv.Settings) {
		if userID == "" {
			return nil
		}
		uc, _ := m.store.GetUserCredentials(ctx, srv.ID, userID)
		hasContextCreds := contextCreds != nil && (contextCreds.APIKey != "" || len(contextCreds.Headers) > 0 || len(contextCreds.Env) > 0)
		if !hasContextCreds && (uc == nil || (uc.APIKey == "" && len(uc.Headers) == 0 && len(uc.Env) == 0)) {
			slog.Debug("mcp.skip_no_user_credentials", "server", srv.Name, "user", userID)
			return nil
		}
	}

	args := jsonBytesToStringSlice(srv.Args)
	env := jsonBytesToStringMap(srv.Env)
	headers, err := resolveEnvVars(jsonBytesToStringMap(srv.Headers))
	if err != nil {
		slog.Warn("security.mcp.env_var_rejected", "server", srv.Name, "err", err)
		return nil
	}

	oauthActive := isOAuthActive(srv.Settings)

	// Inject APIKey into headers if present — ONLY for non-OAuth servers.
	// For OAuth servers the Authorization MUST come from the OAuth token; using the
	// server-level api_key as a fallback would expose tools before authorization.
	if !oauthActive && srv.APIKey != "" && headers["Authorization"] == "" {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["Authorization"] = "Bearer " + srv.APIKey
	}

	if contextCreds != nil {
		if contextCreds.APIKey != "" {
			if headers == nil {
				headers = make(map[string]string)
			}
			headers["Authorization"] = "Bearer " + contextCreds.APIKey
		}
		for k, v := range contextCreds.Headers {
			if headers == nil {
				headers = make(map[string]string)
			}
			headers[k] = v
		}
		for k, v := range contextCreds.Env {
			if env == nil {
				env = make(map[string]string)
			}
			env[k] = v
		}
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

	// OAuth-enabled servers: the OAuth token is the ONLY source of Authorization.
	// Require a valid token — if none is available (not authorized yet / refresh
	// failed / OAuth subsystem absent), skip the server so it is NOT loaded into the
	// shared registry with the server-level credential as a fallback.
	if oauthActive {
		if m.oauthTokenProvider == nil {
			slog.Debug("mcp.skip_oauth_no_provider", "server", srv.Name)
			return nil
		}
		tenantID := store.TenantIDFromContext(ctx)
		oauthUserID := ""
		if requireUserCreds(srv.Settings) {
			oauthUserID = userID
		}
		token, err2 := m.oauthTokenProvider.GetValidToken(ctx, srv.ID, tenantID, oauthUserID)
		if err2 != nil || token == "" {
			slog.Debug("mcp.skip_oauth_no_token", "server", srv.Name, "user", oauthUserID, "error", err2)
			return nil
		}
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["Authorization"] = "Bearer " + token
	}

	// Per-user credentials change connection params → can't share pool connection.
	// Fall back to per-agent mode when user has custom credentials.
	hasUserCreds := contextCreds != nil && (contextCreds.APIKey != "" || len(contextCreds.Headers) > 0 || len(contextCreds.Env) > 0)
	if userID != "" && m.store != nil {
		if uc, _ := m.store.GetUserCredentials(ctx, srv.ID, userID); uc != nil && (uc.APIKey != "" || len(uc.Headers) > 0 || len(uc.Env) > 0) {
			hasUserCreds = true
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

// connectAndFilter establishes the MCP connection (pool or per-agent mode).
// Tool allow/deny filtering from server grants is applied upfront inside
// registerBridgeTools / registerPoolBridgeTools so non-allowed tools never
// reach the registry (and thus never reach the LLM).
func (m *Manager) connectAndFilter(ctx context.Context, rs *resolvedServer) error {
	srv := rs.info.Server
	hints := ParseToolHints(srv.Settings)

	if m.pool != nil && !rs.hasUserCreds {
		// Pool mode: acquire shared connection, create per-agent BridgeTools
		tid := store.TenantIDFromContext(ctx)
		return m.connectViaPool(ctx, tid, srv.Name, srv.Transport, srv.Command,
			rs.args, rs.env, srv.URL, rs.headers, srv.ToolPrefix, srv.TimeoutSec, srv.ID, hints,
			rs.info.ToolAllow, rs.info.ToolDeny)
	}
	// Per-agent mode: create per-agent connection
	return m.connectServer(ctx, srv.Name, srv.Transport, srv.Command,
		rs.args, rs.env, srv.URL, rs.headers,
		srv.ToolPrefix, srv.TimeoutSec, srv.ID, hints,
		rs.info.ToolAllow, rs.info.ToolDeny)
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
	m.registry.RegisterToolGroup("mcp", inlineNames)
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

	m.registry.RegisterToolGroup("mcp", activeNames)
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
	m.registry.RegisterToolGroup("mcp", activeNames)
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
			// Use atomic pointer — health loop may swap client via fullReconnect concurrently.
			if client := ss.clientPtr.Load(); client != nil {
				if err := client.Close(); err != nil {
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

// MCPToolPreviewInfo describes an MCP tool as seen from the store configuration,
// without requiring a live connection to the MCP server.
type MCPToolPreviewInfo struct {
	// RegisteredName is the tool name as it appears in the tool registry (with mcp_ prefix).
	RegisteredName string
	// Description is derived from server tool hints, if configured.
	Description string
	// Parameters is the tool's cached JSON Schema for input parameters, captured
	// at connect-time (see buildCachedToolInfo in manager_connect.go). nil when
	// no schema has been cached yet (server never connected, or cache predates
	// schema capture).
	Parameters json.RawMessage
}

// mcpPreviewDiscoveryTimeout bounds the on-demand tool discovery
// ListToolsForAgent performs when a server has never been live-connected for
// this agent (empty tool_cache and empty registry). Kept short since it's on
// the hot path of a prompt-preview HTTP request, unlike the longer
// discoverToolsTimeout used by the dedicated admin "test connection"/"browse
// tools" endpoints.
const mcpPreviewDiscoveryTimeout = 5 * time.Second

// bareMCPToolName strips a persisted "{effectivePrefix}__" prefix from a
// stored tool_allow/tool_deny entry, if present.
//
// Historically, some agent grants were captured while their MCP server was
// already live-connected and ended up storing the registered (prefixed) tool
// name instead of the bare original MCP tool name — see ServerToolInfos'
// doc comment for how that mismatch happened. Those legacy rows are never
// rewritten automatically, so ListToolsForAgent must accept both shapes
// going forward: bare names (the current, correct shape) and prefixed names
// (already-persisted legacy grants), normalizing to bare before matching
// against tool_cache keys or the live tool registry.
func bareMCPToolName(stored, effectivePrefix string) string {
	if bare, ok := strings.CutPrefix(stored, effectivePrefix+"__"); ok {
		return bare
	}
	return stored
}

// resolveMCPToolInfo resolves the description and parameter schema for a bare
// MCP tool name, preferring the CURRENT live registry entry (if the server is
// connected right now) over the settings-persisted tool_cache snapshot, which
// can be stale or entirely absent. Admin-authored hints always take priority
// over both sources; the server's global hint is the last-resort fallback.
func (m *Manager) resolveMCPToolInfo(toolName, effectivePrefix string, hints ToolHints, toolCache map[string]store.CachedToolInfo) (string, json.RawMessage) {
	desc := hints.HintFor(toolName)
	var params json.RawMessage

	if tool, ok := m.registry.Get(effectivePrefix + "__" + toolName); ok {
		if bridgeTool, isBridge := tool.(*BridgeTool); isBridge {
			if desc == "" {
				desc = bridgeTool.Description()
			}
			if schema := bridgeTool.Parameters(); schema != nil {
				if schemaJSON, err := json.Marshal(schema); err == nil {
					params = schemaJSON
				}
			}
		}
	}

	cached := toolCache[toolName]
	if desc == "" {
		desc = cached.Description
	}
	if params == nil {
		params = cached.Parameters
	}
	if desc == "" && hints.Global != "" {
		desc = hints.Global
	}
	return desc, params
}

// ListToolsForAgent returns a best-effort list of MCP tool names and descriptions
// for a given agent+user based on store configuration only — no actual MCP
// server connections are made. It is intended for prompt preview.
//
// For each accessible server:
//   - If the agent grant has an explicit ToolAllow list, those tool names are
//     used (minus any ToolDeny entries).
//   - If ToolAllow is empty (all tools allowed), only a single placeholder entry
//     is returned for the server (the exact tool list is unknown without connecting).
//
// Per-tool descriptions are populated from the server's tool_hints settings when present.
func (m *Manager) ListToolsForAgent(ctx context.Context, agentID uuid.UUID, userID string) ([]MCPToolPreviewInfo, error) {
	slog.Debug("mcp.ListToolsForAgent.called", "agent_id", agentID, "user_id", userID)

	if m.store == nil {
		slog.Debug("mcp.ListToolsForAgent.no_store", "agent_id", agentID)
		return nil, nil
	}

	accessible, err := m.store.ListAccessible(ctx, agentID, userID)
	if err != nil {
		return nil, fmt.Errorf("list accessible MCP servers: %w", err)
	}

	slog.Debug("mcp.ListToolsForAgent.accessible_servers", "agent_id", agentID, "count", len(accessible))

	var result []MCPToolPreviewInfo
	for _, info := range accessible {
		slog.Debug("mcp.ListToolsForAgent.server", "server", info.Server.Name, "enabled", info.Server.Enabled, "tool_allow_count", len(info.ToolAllow), "tool_deny_count", len(info.ToolDeny), "has_settings", len(info.Server.Settings) > 0)
		if !info.Server.Enabled {
			slog.Debug("mcp.ListToolsForAgent.server_disabled", "server", info.Server.Name)
			continue
		}
		hints := ParseToolHints(info.Server.Settings)
		effectivePrefix := ensureMCPPrefix(info.Server.ToolPrefix, info.Server.Name)
		slog.Debug("mcp.ListToolsForAgent.server_hints", "server", info.Server.Name, "global_hint", hints.Global, "tool_hints_count", len(hints.Tools), "effective_prefix", effectivePrefix)

		// Parse tool cache from settings as fallback descriptions + parameter schemas.
		toolCache := make(map[string]store.CachedToolInfo)
		if len(info.Server.Settings) > 0 {
			var settingsMap map[string]json.RawMessage
			if err := json.Unmarshal(info.Server.Settings, &settingsMap); err == nil {
				if cacheRaw, ok := settingsMap["tool_cache"]; ok {
					if err := json.Unmarshal(cacheRaw, &toolCache); err != nil {
						// Backward-compat: pre-schema-caching rows stored a bare
						// map[string]string (name -> description). Fall back to
						// that shape and treat entries as description-only (no
						// parameter schema). Stale rows self-heal on next connect
						// since the write path always writes the new shape.
						var legacyCache map[string]string
						if legacyErr := json.Unmarshal(cacheRaw, &legacyCache); legacyErr == nil {
							toolCache = make(map[string]store.CachedToolInfo, len(legacyCache))
							for name, desc := range legacyCache {
								toolCache[name] = store.CachedToolInfo{Description: desc}
							}
						} else {
							slog.Debug("mcp.ListToolsForAgent.tool_cache_unmarshal_failed", "server", info.Server.Name, "error", err)
						}
					}
				}
			}
		}

		// Nothing persisted yet and no live registry entries either (the
		// server has never actually been connected for this agent, e.g. a
		// freshly added MCP server before the agent's first real chat turn
		// triggers Manager.LoadForAgent). ListToolsForAgent is otherwise
		// documented as "no actual MCP server connections are made", which
		// left the prompt preview permanently showing empty descriptions and
		// "{type: object}" schemas until a real session happened to connect.
		// Do a single bounded on-demand discovery here (same live path the
		// admin "browse tools" endpoint already uses, see handleListServerTools
		// in internal/http/mcp_tools.go) so the preview reflects real tool
		// data immediately, and persist it so subsequent calls hit the cache.
		if len(toolCache) == 0 && len(m.ServerToolInfos(info.Server.Name)) == 0 {
			if rs := m.resolveServerCredentials(ctx, info, userID); rs != nil {
				discovered, err := discoverRawTools(ctx, info.Server.Transport, info.Server.Command, rs.args, rs.env, info.Server.URL, rs.headers, mcpPreviewDiscoveryTimeout)
				if err != nil {
					slog.Debug("mcp.ListToolsForAgent.on_demand_discovery_failed", "server", info.Server.Name, "error", err)
				} else {
					toolCache = buildCachedToolInfo(discovered)
					if info.Server.ID != uuid.Nil && m.store != nil && len(toolCache) > 0 {
						go func(sid uuid.UUID, cache map[string]store.CachedToolInfo) {
							cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							if cacheErr := m.store.CacheToolDescriptions(cacheCtx, sid, cache); cacheErr != nil {
								slog.Debug("mcp.ListToolsForAgent.cache_tool_descriptions_failed", "server_id", sid, "error", cacheErr)
							}
						}(info.Server.ID, toolCache)
					}
				}
			}
		}

		// Build deny set, normalizing legacy prefixed entries to bare names
		// (see bareMCPToolName) so they match tool_cache/registry lookups.
		denySet := make(map[string]struct{}, len(info.ToolDeny))
		for _, d := range info.ToolDeny {
			denySet[bareMCPToolName(d, effectivePrefix)] = struct{}{}
		}

		if len(info.ToolAllow) == 0 {
			if len(toolCache) > 0 {
				// Unrestricted grant, but we have real tool names cached from a
				// prior connection — enumerate them instead of a placeholder.
				var serverTools []string
				for toolName := range toolCache {
					if _, denied := denySet[toolName]; denied {
						slog.Debug("mcp.ListToolsForAgent.tool_denied", "server", info.Server.Name, "tool", toolName)
						continue
					}
					registeredName := effectivePrefix + "__" + toolName
					desc, params := m.resolveMCPToolInfo(toolName, effectivePrefix, hints, toolCache)
					serverTools = append(serverTools, registeredName)
					result = append(result, MCPToolPreviewInfo{
						RegisteredName: registeredName,
						Description:    desc,
						Parameters:     params,
					})
				}
				slog.Debug("mcp.ListToolsForAgent.server_tools_added_from_cache", "server", info.Server.Name, "tools", serverTools)
				continue
			}

			// Unknown tool list and nothing cached — emit one placeholder entry.
			placeholder := effectivePrefix + "__*"
			desc := hints.Global
			if desc == "" {
				desc = "MCP server: " + info.Server.Name
			}
			slog.Debug("mcp.ListToolsForAgent.placeholder_entry", "server", info.Server.Name, "placeholder", placeholder)
			result = append(result, MCPToolPreviewInfo{
				RegisteredName: placeholder,
				Description:    desc,
			})
			continue
		}

		var serverTools []string
		for _, storedName := range info.ToolAllow {
			// Normalize legacy prefixed entries (persisted before the grant-capture
			// fix in mcp_tools.go) to the bare tool name so they resolve against
			// tool_cache and the live registry the same as current bare entries.
			toolName := bareMCPToolName(storedName, effectivePrefix)
			if _, denied := denySet[toolName]; denied {
				slog.Debug("mcp.ListToolsForAgent.tool_denied", "server", info.Server.Name, "tool", toolName)
				continue
			}
			registeredName := effectivePrefix + "__" + toolName
			desc, params := m.resolveMCPToolInfo(toolName, effectivePrefix, hints, toolCache)
			serverTools = append(serverTools, registeredName)
			result = append(result, MCPToolPreviewInfo{
				RegisteredName: registeredName,
				Description:    desc,
				Parameters:     params,
			})
		}
		slog.Debug("mcp.ListToolsForAgent.server_tools_added", "server", info.Server.Name, "tools", serverTools)
	}

	slog.Info("mcp.ListToolsForAgent.result", "agent_id", agentID, "user_id", userID, "total_tools", len(result))
	return result, nil
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
// Uses fail-closed validation: only allowlisted env vars are permitted.
func resolveEnvVars(m map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(m))
	for k, v := range m {
		resolved, err := ValidateAndResolveEnvVar(v)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", k, err)
		}
		out[k] = resolved
	}
	return out, nil
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

// isOAuthActive checks if the server's settings have OAuth auth_type enabled.
func isOAuthActive(settings json.RawMessage) bool {
	return IsOAuthActive(settings)
}

// IsOAuthActive reports whether the given raw server settings JSON has OAuth enabled.
// Exported for use by HTTP handlers that need to gate OAuth token injection.
func IsOAuthActive(settings json.RawMessage) bool {
	if len(settings) == 0 {
		return false
	}
	var s struct {
		OAuth struct {
			AuthType string `json:"auth_type"`
		} `json:"oauth"`
	}
	_ = json.Unmarshal(settings, &s)
	return s.OAuth.AuthType == "oauth"
}

// ToolHints carries admin-authored description hints for MCP tools.
// Stored under MCPServerData.Settings.tool_hints as JSONB:
//
//	{
//	  "tool_hints": {
//	    "global": "...",
//	    "tools": { "<tool_name>": "..." }
//	  }
//	}
//
// The hints are appended to a tool's description so the LLM sees server-specific
// quirks (e.g. "no trailing semicolons in code args") without modifying the MCP
// server itself. Empty Global/Tools render no suffix.
type ToolHints struct {
	Global string            `json:"global,omitempty"`
	Tools  map[string]string `json:"tools,omitempty"`
}

// ParseToolHints extracts tool description hints from an MCP server's Settings JSONB.
// Returns a zero-value ToolHints (no hints) if settings are empty or malformed.
// Safe to call with nil — never panics.
func ParseToolHints(settings json.RawMessage) ToolHints {
	if len(settings) == 0 {
		return ToolHints{}
	}
	var s struct {
		ToolHints ToolHints `json:"tool_hints"`
	}
	_ = json.Unmarshal(settings, &s)
	return s.ToolHints
}

// HintFor returns the per-tool hint for toolName, or empty string if none.
func (h ToolHints) HintFor(toolName string) string {
	if h.Tools == nil {
		return ""
	}
	return h.Tools[toolName]
}
