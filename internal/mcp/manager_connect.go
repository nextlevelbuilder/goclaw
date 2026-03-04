package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

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
		Name:    "goclaw",
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

	// Create health monitoring context BEFORE storing server in map,
	// so DisconnectServer() can safely call ss.cancel even if called immediately.
	hctx, hcancel := context.WithCancel(context.Background())
	ss.cancel = hcancel

	// Store server state BEFORE updating MCP group, because updateMCPGroup()
	// calls ToolNames() which iterates m.servers. If we store after, the current
	// server's tools are invisible to ToolNames() and toolGroups["mcp"] ends up
	// incomplete — causing the policy engine to block MCP tools.
	m.mu.Lock()
	m.servers[name] = ss
	m.mu.Unlock()

	// Register dynamic tool groups for policy filtering
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
