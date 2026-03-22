package mcp

import (
	"context"
	"log/slog"
	"sync"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// poolEntry holds a shared connection and its discovered tools.
type poolEntry struct {
	state    *serverState // connection + health state
	tools    []mcpgo.Tool // discovered MCP tool definitions
	refCount int          // number of active Manager references
}

// Pool manages shared MCP server connections across agents.
// One physical connection per server name, shared by all agents
// that have grants to that server. Each agent creates its own
// BridgeTools pointing to the shared client/connected pointers.
type Pool struct {
	mu      sync.Mutex
	servers map[string]*poolEntry
}

// NewPool creates a shared MCP connection pool.
func NewPool() *Pool {
	return &Pool{
		servers: make(map[string]*poolEntry),
	}
}

// Acquire returns a shared connection for the named server.
// If no connection exists, it connects using the provided config.
// Increments the reference count.
// poolKey is the composite key (e.g. "name" or "name:projectID") used for
// process isolation; name is the server name used for connectAndDiscover.
func (p *Pool) Acquire(ctx context.Context, poolKey, name, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, timeoutSec int) (*poolEntry, error) {
	p.mu.Lock()

	if entry, ok := p.servers[poolKey]; ok && entry.state.connected.Load() {
		entry.refCount++
		p.mu.Unlock()
		slog.Debug("mcp.pool.reuse", "server", name, "poolKey", poolKey, "refCount", entry.refCount)
		return entry, nil
	}

	// If entry exists but disconnected, close old connection first
	if old, ok := p.servers[poolKey]; ok {
		if old.state.cancel != nil {
			old.state.cancel()
		}
		if old.state.client != nil {
			_ = old.state.client.Close()
		}
		delete(p.servers, poolKey)
	}

	p.mu.Unlock()

	// Connect outside the lock (may be slow)
	ss, mcpTools, err := connectAndDiscover(ctx, name, transportType, command, args, env, url, headers, timeoutSec)
	if err != nil {
		return nil, err
	}

	// Start health loop
	hctx, hcancel := context.WithCancel(context.Background())
	ss.cancel = hcancel
	go poolHealthLoop(hctx, ss)

	entry := &poolEntry{
		state:    ss,
		tools:    mcpTools,
		refCount: 1,
	}

	p.mu.Lock()
	// Check if another goroutine connected while we were connecting
	if existing, ok := p.servers[poolKey]; ok && existing.state.connected.Load() {
		// Use existing, close ours
		p.mu.Unlock()
		hcancel()
		_ = ss.client.Close()
		p.mu.Lock()
		existing.refCount++
		p.mu.Unlock()
		return existing, nil
	}
	p.servers[poolKey] = entry
	p.mu.Unlock()

	slog.Info("mcp.pool.connected", "server", name, "poolKey", poolKey, "tools", len(mcpTools))
	return entry, nil
}

// Release decrements the reference count for a server.
// The connection is NOT closed when refCount reaches 0 — it stays
// alive for future agents. Use Stop() to close all connections.
func (p *Pool) Release(poolKey string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.servers[poolKey]; ok {
		entry.refCount--
		if entry.refCount < 0 {
			entry.refCount = 0
		}
		slog.Debug("mcp.pool.release", "poolKey", poolKey, "refCount", entry.refCount)
	}
}

// Stop closes all pooled connections. Called on gateway shutdown.
func (p *Pool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, entry := range p.servers {
		if entry.state.cancel != nil {
			entry.state.cancel()
		}
		if entry.state.client != nil {
			_ = entry.state.client.Close()
		}
		slog.Debug("mcp.pool.stopped", "server", name)
	}
	p.servers = make(map[string]*poolEntry)
}

// poolHealthLoop is a standalone health loop for pool-managed connections.
// Unlike Manager.healthLoop, it doesn't trigger reconnection via Manager
// since the pool owns the connection lifecycle.
func poolHealthLoop(ctx context.Context, ss *serverState) {
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
					continue
				}
				ss.connected.Store(false)
				ss.mu.Lock()
				ss.lastErr = err.Error()
				ss.mu.Unlock()
				slog.Warn("mcp.pool.health_failed", "server", ss.name, "error", err)
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
