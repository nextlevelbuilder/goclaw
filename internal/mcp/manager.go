package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
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
// Supports two modes:
//   - Standalone: reads from config.MCPServerConfig map (shared across all agents)
//   - Managed: queries MCPServerStore per agent+user for permission-filtered servers
type Manager struct {
	mu       sync.RWMutex
	servers  map[string]*serverState
	registry *tools.Registry

	// Standalone mode
	configs map[string]*config.MCPServerConfig

	// Managed mode
	store store.MCPServerStore
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithConfigs sets static MCP server configs (standalone mode).
func WithConfigs(cfgs map[string]*config.MCPServerConfig) ManagerOption {
	return func(m *Manager) {
		m.configs = cfgs
	}
}

// WithStore sets the MCPServerStore for managed mode.
func WithStore(s store.MCPServerStore) ManagerOption {
	return func(m *Manager) {
		m.store = s
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

// Start connects to all configured MCP servers (standalone mode).
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

// LoadForAgent connects MCP servers accessible by a specific agent+user (managed mode).
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

		if err := m.connectServer(ctx, srv.Name, srv.Transport, srv.Command,
			jsonBytesToStringSlice(srv.Args), jsonBytesToStringMap(srv.Env),
			srv.URL, jsonBytesToStringMap(srv.Headers),
			srv.ToolPrefix, srv.TimeoutSec); err != nil {
			slog.Warn("mcp.server.connect_failed", "server", srv.Name, "error", err)
			continue
		}

		// Apply tool filtering from grants
		if len(info.ToolAllow) > 0 || len(info.ToolDeny) > 0 {
			m.filterTools(srv.Name, info.ToolAllow, info.ToolDeny)
		}
	}

	return nil
}

// Stop shuts down all MCP server connections and unregisters tools.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, ss := range m.servers {
		if ss.cancel != nil {
			ss.cancel()
		}
		if ss.client != nil {
			if err := ss.client.Close(); err != nil {
				slog.Debug("mcp.server.close_error", "server", name, "error", err)
			}
		}
		// Unregister tools
		for _, toolName := range ss.toolNames {
			m.registry.Unregister(toolName)
		}
	}
	m.servers = make(map[string]*serverState)
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

// ToolNames returns all registered MCP tool names.
func (m *Manager) ToolNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for _, ss := range m.servers {
		names = append(names, ss.toolNames...)
	}
	return names
}

// connectServer creates a client, initializes the connection, discovers tools, and registers them.
func (m *Manager) connectServer(ctx context.Context, name, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, toolPrefix string, timeoutSec int) error {
	client, err := createClient(transportType, command, args, env, url, headers)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	// Start transport (SSE/streamable-http need explicit Start; stdio auto-starts)
	if transportType != "stdio" {
		if err := client.Start(ctx); err != nil {
			_ = client.Close()
			return fmt.Errorf("start transport: %w", err)
		}
	}

	// Initialize MCP handshake
	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{
		Name:    "openclaw-go",
		Version: "1.0.0",
	}

	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	// Discover tools
	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("list tools: %w", err)
	}

	if timeoutSec <= 0 {
		timeoutSec = 60
	}

	ss := &serverState{
		name:       name,
		transport:  transportType,
		client:     client,
		timeoutSec: timeoutSec,
	}
	ss.connected.Store(true)

	// Register tools
	var registeredNames []string
	for _, mcpTool := range toolsResult.Tools {
		bt := NewBridgeTool(name, mcpTool, client, toolPrefix, timeoutSec, &ss.connected)

		// Check for name collision with existing tools
		if _, exists := m.registry.Get(bt.Name()); exists {
			slog.Warn("mcp.tool.name_collision",
				"server", name,
				"tool", bt.Name(),
				"action", "skipped",
			)
			continue
		}

		m.registry.Register(bt)
		registeredNames = append(registeredNames, bt.Name())
	}
	ss.toolNames = registeredNames

	// Register dynamic tool groups for policy filtering
	if len(registeredNames) > 0 {
		tools.RegisterToolGroup("mcp:"+name, registeredNames)
		m.updateMCPGroup()
	}

	// Start health monitoring
	hctx, hcancel := context.WithCancel(context.Background())
	ss.cancel = hcancel
	go m.healthLoop(hctx, ss)

	m.mu.Lock()
	m.servers[name] = ss
	m.mu.Unlock()

	slog.Info("mcp.server.connected",
		"server", name,
		"transport", transportType,
		"tools", len(registeredNames),
	)

	return nil
}

// createClient creates the appropriate MCP client based on transport type.
func createClient(transportType, command string, args []string, env map[string]string, url string, headers map[string]string) (*mcpclient.Client, error) {
	switch transportType {
	case "stdio":
		envSlice := mapToEnvSlice(env)
		return mcpclient.NewStdioMCPClient(command, envSlice, args...)

	case "sse":
		var opts []transport.ClientOption
		if len(headers) > 0 {
			opts = append(opts, mcpclient.WithHeaders(headers))
		}
		return mcpclient.NewSSEMCPClient(url, opts...)

	case "streamable-http":
		var opts []transport.StreamableHTTPCOption
		if len(headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(headers))
		}
		return mcpclient.NewStreamableHttpClient(url, opts...)

	default:
		return nil, fmt.Errorf("unsupported transport: %q", transportType)
	}
}

// healthLoop periodically pings the MCP server and attempts reconnection on failure.
func (m *Manager) healthLoop(ctx context.Context, ss *serverState) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := ss.client.Ping(ctx); err != nil {
				// Servers that don't implement "ping" are still alive — treat as healthy.
				if strings.Contains(strings.ToLower(err.Error()), "method not found") {
					ss.connected.Store(true)
					ss.mu.Lock()
					ss.reconnAttempts = 0
					ss.lastErr = ""
					ss.mu.Unlock()
					continue
				}
				ss.connected.Store(false)
				ss.mu.Lock()
				ss.lastErr = err.Error()
				ss.mu.Unlock()

				slog.Warn("mcp.server.health_failed", "server", ss.name, "error", err)
				m.tryReconnect(ctx, ss)
			} else {
				ss.connected.Store(true)
				ss.mu.Lock()
				ss.reconnAttempts = 0
				ss.lastErr = ""
				ss.mu.Unlock()
			}
		}
	}
}

// tryReconnect attempts to reconnect with exponential backoff.
func (m *Manager) tryReconnect(ctx context.Context, ss *serverState) {
	ss.mu.Lock()
	if ss.reconnAttempts >= maxReconnectAttempts {
		ss.lastErr = fmt.Sprintf("max reconnect attempts (%d) reached", maxReconnectAttempts)
		ss.mu.Unlock()
		slog.Error("mcp.server.reconnect_exhausted", "server", ss.name)
		return
	}
	ss.reconnAttempts++
	attempt := ss.reconnAttempts
	ss.mu.Unlock()

	backoff := initialBackoff * time.Duration(1<<(attempt-1))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	slog.Info("mcp.server.reconnecting",
		"server", ss.name,
		"attempt", attempt,
		"backoff", backoff,
	)

	select {
	case <-ctx.Done():
		return
	case <-time.After(backoff):
	}

	// Try to ping again — transport may have auto-reconnected
	if err := ss.client.Ping(ctx); err == nil {
		ss.connected.Store(true)
		ss.mu.Lock()
		ss.reconnAttempts = 0
		ss.lastErr = ""
		ss.mu.Unlock()
		slog.Info("mcp.server.reconnected", "server", ss.name)
	}
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

	for name, ss := range m.servers {
		if ss.cancel != nil {
			ss.cancel()
		}
		if ss.client != nil {
			_ = ss.client.Close()
		}
		for _, toolName := range ss.toolNames {
			m.registry.Unregister(toolName)
		}
		tools.UnregisterToolGroup("mcp:" + name)
		slog.Debug("mcp.server.unregistered", "server", name, "tools", len(ss.toolNames))
	}
	m.servers = make(map[string]*serverState)
	tools.UnregisterToolGroup("mcp")
}

// filterTools removes tools from the registry that don't match the allow/deny lists.
func (m *Manager) filterTools(serverName string, allow, deny []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ss, ok := m.servers[serverName]
	if !ok {
		return
	}

	allowSet := toSet(allow)
	denySet := toSet(deny)

	var kept []string
	for _, toolName := range ss.toolNames {
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
	ss.toolNames = kept
}

// --- helpers ---

func mapToEnvSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	s := make([]string, 0, len(env))
	for k, v := range env {
		s = append(s, k+"="+v)
	}
	return s
}

func toSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(items))
	for _, item := range items {
		s[item] = struct{}{}
	}
	return s
}

func joinErrors(errs []string) string {
	result := ""
	for i, e := range errs {
		if i > 0 {
			result += "; "
		}
		result += e
	}
	return result
}

// jsonBytesToStringSlice converts JSONB []byte to []string. Returns nil on error.
func jsonBytesToStringSlice(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var result []string
	if err := jsonUnmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// jsonBytesToStringMap converts JSONB []byte to map[string]string. Returns nil on error.
func jsonBytesToStringMap(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var result map[string]string
	if err := jsonUnmarshal(data, &result); err != nil {
		return nil
	}
	return result
}
