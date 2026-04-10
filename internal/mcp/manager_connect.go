package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// connectAndDiscover creates a client, initializes the MCP handshake, and
// discovers tools. Returns a connected serverState with discovered tool
// definitions. The caller is responsible for registering tools and starting
// the health loop. This function is shared by both Manager and Pool.
func connectAndDiscover(ctx context.Context, name, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, timeoutSec int) (*serverState, []mcpgo.Tool, error) {
	client, err := createClient(transportType, command, args, env, url, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("create client: %w", err)
	}

	if transportType != "stdio" {
		if err := client.Start(ctx); err != nil {
			_ = client.Close()
			return nil, nil, fmt.Errorf("start transport: %w", err)
		}
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{
		Name:    "goclaw",
		Version: "1.0.0",
	}

	// Retry initialization with exponential backoff for slow-starting stdio servers.
	// Heavy MCP servers (FastMCP with 80+ tools, OAuth servers) can take 3-5s to start
	// their stdin read loop. Without retries, Initialize sends JSON-RPC before the
	// server is ready, gets EOF, and permanently fails. SSE/HTTP transports don't need
	// this because the HTTP server rejects connections until ready (connection refused).
	const maxInitAttempts = 4 // backoff: 2s + 4s + 8s = ~14s total before giving up
	var initErr error
	for attempt := range maxInitAttempts {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * time.Second // 2s, 4s, 8s
			slog.Debug("mcp.init.retry", "server", name, "attempt", attempt+1, "backoff", backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				_ = client.Close()
				return nil, nil, fmt.Errorf("initialize: context cancelled during retry: %w", ctx.Err())
			}
		}
		if _, err := client.Initialize(ctx, initReq); err == nil {
			break
		} else {
			initErr = err
			// Non-stdio transports: connection errors are definitive, don't retry.
			if transportType != "stdio" {
				break
			}
		}
	}
	if initErr != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("initialize: %w", initErr)
	}

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}

	if timeoutSec <= 0 {
		timeoutSec = 60
	}

	ss := &serverState{
		name:       name,
		transport:  transportType,
		client:     client,
		timeoutSec: timeoutSec,
		conn: connParams{
			command: command,
			args:    args,
			env:     env,
			url:     url,
			headers: headers,
		},
	}
	ss.clientPtr.Store(client)
	ss.connected.Store(true)

	return ss, toolsResult.Tools, nil
}

// connectServer creates a client, initializes the connection, discovers tools, and registers them.
func (m *Manager) connectServer(ctx context.Context, name, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, toolPrefix string, timeoutSec int) error {
	ss, mcpTools, err := connectAndDiscover(ctx, name, transportType, command, args, env, url, headers, timeoutSec)
	if err != nil {
		return err
	}

	// Register tools
	registeredNames := m.registerBridgeTools(ss, mcpTools, name, toolPrefix, timeoutSec)
	ss.toolNames = registeredNames

	// Create health monitoring context
	hctx, hcancel := context.WithCancel(context.Background())
	ss.cancel = hcancel

	// Store server state BEFORE updating MCP group
	m.mu.Lock()
	m.servers[name] = ss
	m.mu.Unlock()

	if len(registeredNames) > 0 {
		tools.RegisterToolGroup("mcp:"+name, registeredNames)
		m.updateMCPGroup()
	}

	go m.healthLoop(hctx, ss)

	slog.Info("mcp.server.connected",
		"server", name,
		"transport", transportType,
		"tools", len(registeredNames),
	)

	return nil
}

// registerBridgeTools creates BridgeTools from MCP tool definitions and
// registers them in the Manager's registry. Returns registered tool names.
func (m *Manager) registerBridgeTools(ss *serverState, mcpTools []mcpgo.Tool, serverName, toolPrefix string, timeoutSec int) []string {
	var registeredNames []string
	for _, mcpTool := range mcpTools {
		bt := NewBridgeTool(serverName, mcpTool, &ss.clientPtr, toolPrefix, timeoutSec, &ss.connected)

		if _, exists := m.registry.Get(bt.Name()); exists {
			slog.Warn("mcp.tool.name_collision",
				"server", serverName,
				"tool", bt.Name(),
				"action", "skipped",
			)
			continue
		}

		m.registry.Register(bt)
		registeredNames = append(registeredNames, bt.Name())
	}
	return registeredNames
}

// connectViaPool acquires a shared connection from the pool and creates
// per-agent BridgeTools pointing to the shared client/connected pointers.
func (m *Manager) connectViaPool(ctx context.Context, tenantID uuid.UUID, name, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, toolPrefix string, timeoutSec int) error {
	entry, err := m.pool.Acquire(ctx, tenantID, name, transportType, command, args, env, url, headers, timeoutSec)
	if err != nil {
		return err
	}

	// Create per-agent BridgeTools from the pool's shared connection
	registeredNames := m.registerPoolBridgeTools(entry, name, toolPrefix, timeoutSec)

	// Track server state and per-agent tool names.
	// poolServers/poolToolNames keyed by plain name for Close() iteration.
	// poolKeys maps plain name → pool compound key for Release().
	m.mu.Lock()
	m.servers[name] = entry.state
	if m.poolServers == nil {
		m.poolServers = make(map[string]struct{})
	}
	m.poolServers[name] = struct{}{}
	if m.poolToolNames == nil {
		m.poolToolNames = make(map[string][]string)
	}
	m.poolToolNames[name] = registeredNames
	if m.poolKeys == nil {
		m.poolKeys = make(map[string]string)
	}
	m.poolKeys[name] = poolKey(tenantID, name)
	m.mu.Unlock()

	if len(registeredNames) > 0 {
		tools.RegisterToolGroup("mcp:"+name, registeredNames)
		m.updateMCPGroup()
	}

	slog.Info("mcp.server.connected_via_pool",
		"server", name,
		"transport", transportType,
		"tools", len(registeredNames),
	)

	return nil
}

// registerPoolBridgeTools creates BridgeTools from pool entry's discovered tools,
// pointing to the shared client/connected pointers. Returns registered tool names.
func (m *Manager) registerPoolBridgeTools(entry *poolEntry, serverName, toolPrefix string, timeoutSec int) []string {
	var registeredNames []string
	for _, mcpTool := range entry.tools {
		bt := NewBridgeTool(serverName, mcpTool, &entry.state.clientPtr, toolPrefix, timeoutSec, &entry.state.connected)

		if _, exists := m.registry.Get(bt.Name()); exists {
			slog.Warn("mcp.tool.name_collision",
				"server", serverName,
				"tool", bt.Name(),
				"action", "skipped",
			)
			continue
		}

		m.registry.Register(bt)
		registeredNames = append(registeredNames, bt.Name())
	}

	return registeredNames
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

// newHealthTicker creates a ticker for health check intervals.
func newHealthTicker() *time.Ticker {
	return time.NewTicker(healthCheckInterval)
}

// isMethodNotFound returns true if the error indicates the server
// doesn't implement the "ping" method (still considered healthy).
func isMethodNotFound(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "method not found")
}

// healthLoop periodically pings the MCP server and attempts reconnection on failure.
func (m *Manager) healthLoop(ctx context.Context, ss *serverState) {
	ticker := newHealthTicker()
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := ss.client.Ping(ctx); err != nil {
				if isMethodNotFound(err) {
					ss.connected.Store(true)
					ss.mu.Lock()
					ss.reconnAttempts = 0
					ss.healthFailures = 0
					ss.lastErr = ""
					ss.mu.Unlock()
					continue
				}
				ss.mu.Lock()
				ss.healthFailures++
				failures := ss.healthFailures
				ss.lastErr = err.Error()
				ss.mu.Unlock()

				slog.Warn("mcp.server.health_failed", "server", ss.name, "error", err, "consecutive", failures)

				// Only mark disconnected and attempt reconnect after consecutive failures
				// to tolerate transient errors (e.g. 504 from upstream proxy).
				if failures >= healthFailThreshold {
					ss.connected.Store(false)
					m.tryReconnect(ctx, ss)
				}
			} else {
				ss.connected.Store(true)
				ss.mu.Lock()
				ss.reconnAttempts = 0
				ss.healthFailures = 0
				ss.lastErr = ""
				ss.mu.Unlock()
			}
		}
	}
}

// tryReconnect attempts to reconnect with exponential backoff.
func (m *Manager) tryReconnect(ctx context.Context, ss *serverState) {
	reconnectWithBackoff(ctx, ss, "mcp.server")
}

// reconnectWithBackoff implements the two-phase reconnect strategy shared by
// Manager.healthLoop and poolHealthLoop. Handles cooldown after exhausting
// max attempts, exponential backoff, fast-path ping (transient blips), and
// slow-path full reconnect (dead server-side session).
// logPrefix distinguishes log entries (e.g. "mcp.server" vs "mcp.pool").
func reconnectWithBackoff(ctx context.Context, ss *serverState, logPrefix string) {
	ss.mu.Lock()
	if ss.reconnAttempts >= maxReconnectAttempts {
		ss.lastErr = fmt.Sprintf("max reconnect attempts (%d) reached, entering cooldown", maxReconnectAttempts)
		ss.mu.Unlock()
		slog.Warn(logPrefix+".reconnect_cooldown", "server", ss.name, "cooldown", reconnectCooldown)
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectCooldown):
		}
		ss.mu.Lock()
		ss.reconnAttempts = 0
		ss.mu.Unlock()
		return // will retry on next health tick
	}
	ss.reconnAttempts++
	attempt := ss.reconnAttempts
	ss.mu.Unlock()

	backoff := min(initialBackoff*time.Duration(1<<(attempt-1)), maxBackoff)
	slog.Info(logPrefix+".reconnecting", "server", ss.name, "attempt", attempt, "backoff", backoff)

	select {
	case <-ctx.Done():
		return
	case <-time.After(backoff):
	}

	// Fast path: ping existing client — works for transient network blips
	// where the server-side session is still alive.
	if err := ss.client.Ping(ctx); err == nil {
		ss.connected.Store(true)
		ss.mu.Lock()
		ss.reconnAttempts = 0
		ss.healthFailures = 0
		ss.lastErr = ""
		ss.mu.Unlock()
		slog.Info(logPrefix+".reconnected", "server", ss.name)
		return
	}

	// Slow path: server-side session is dead (container restart, OOM, etc.).
	if fullReconnect(ctx, ss) {
		slog.Info(logPrefix+".reconnected", "server", ss.name, "method", "full_reconnect")
	}
}

// fullReconnect creates a fresh MCP client, atomically swaps it into serverState,
// and closes the old one. Returns true on success. Used by reconnectWithBackoff
// as the slow path when pinging the old client fails.
//
// The new client is created and validated FIRST, then swapped via clientPtr.Store()
// so BridgeTools see the new client immediately. The old client is closed AFTER
// the swap to avoid a window where ss.client points to a closed client.
//
// NOTE: Does not re-discover tools (ListTools). If the MCP server restarts with
// a different tool set, changes won't be reflected until the Manager reconnects.
func fullReconnect(ctx context.Context, ss *serverState) bool {
	slog.Info("mcp.full_reconnect", "server", ss.name, "transport", ss.transport)

	newClient, err := createClient(ss.transport, ss.conn.command, ss.conn.args, ss.conn.env, ss.conn.url, ss.conn.headers)
	if err != nil {
		slog.Warn("mcp.reconnect_create_failed", "server", ss.name, "error", err)
		return false
	}

	if ss.transport != "stdio" {
		if err := newClient.Start(ctx); err != nil {
			_ = newClient.Close()
			slog.Warn("mcp.reconnect_start_failed", "server", ss.name, "error", err)
			return false
		}
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "goclaw", Version: "1.0.0"}

	if _, err := newClient.Initialize(ctx, initReq); err != nil {
		_ = newClient.Close()
		slog.Warn("mcp.reconnect_init_failed", "server", ss.name, "error", err)
		return false
	}

	// Swap atomically: store new client, then close old.
	// BridgeTools use clientPtr.Load() so they see the new client immediately.
	oldClient := ss.client
	ss.client = newClient
	ss.clientPtr.Store(newClient)
	ss.connected.Store(true)
	ss.mu.Lock()
	ss.reconnAttempts = 0
	ss.healthFailures = 0
	ss.lastErr = ""
	ss.mu.Unlock()

	_ = oldClient.Close()
	return true
}
