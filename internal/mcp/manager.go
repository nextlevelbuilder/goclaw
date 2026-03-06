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
type Manager struct {
	mu       sync.RWMutex
	servers  map[string]*serverState
	registry *tools.Registry

	// Config-based servers
	configs map[string]*config.MCPServerConfig

	// DB-backed servers
	store store.MCPServerStore
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
