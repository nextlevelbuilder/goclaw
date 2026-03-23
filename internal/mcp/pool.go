package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// PoolConfig configures the MCP connection pool.
type PoolConfig struct {
	MaxSize        int           // global max connections (default 100)
	MaxIdle        int           // max idle connections to keep alive (default 20)
	IdleTTL        time.Duration // close idle connections after this (default 20m)
	AcquireTimeout time.Duration // wait for pool slot before error (default 60s)
}

// DefaultPoolConfig returns the default pool configuration.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxSize:        100,
		MaxIdle:        20,
		IdleTTL:        20 * time.Minute,
		AcquireTimeout: 60 * time.Second,
	}
}

// poolEntry holds a shared connection and its discovered tools.
type poolEntry struct {
	state    *serverState // connection + health state
	tools    []mcpgo.Tool // discovered MCP tool definitions
	refCount int          // number of active Manager references
	lastUsed time.Time    // last Acquire/Release time for idle eviction
}

// Pool manages shared MCP server connections across agents.
// Connections are keyed by tenantID/serverName for tenant isolation.
type Pool struct {
	mu      sync.Mutex
	servers map[string]*poolEntry
	cfg     PoolConfig
	slot    chan struct{} // semaphore for MaxSize
	stopCh  chan struct{}
}

// NewPool creates a shared MCP connection pool with idle eviction.
func NewPool(cfg PoolConfig) *Pool {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 100
	}
	if cfg.MaxIdle <= 0 {
		cfg.MaxIdle = 20
	}
	if cfg.IdleTTL <= 0 {
		cfg.IdleTTL = 20 * time.Minute
	}
	if cfg.AcquireTimeout <= 0 {
		cfg.AcquireTimeout = 60 * time.Second
	}

	p := &Pool{
		servers: make(map[string]*poolEntry),
		cfg:     cfg,
		slot:    make(chan struct{}, cfg.MaxSize),
		stopCh:  make(chan struct{}),
	}
	go p.evictLoop()
	return p
}

// poolKey builds a tenant-scoped key for pool lookups.
func poolKey(tenantID uuid.UUID, name string) string {
	return tenantID.String() + "/" + name
}

// Acquire returns a shared connection for the named server scoped to a tenant.
// If no connection exists, it connects using the provided config.
// Blocks up to AcquireTimeout if pool is at MaxSize.
func (p *Pool) Acquire(ctx context.Context, tenantID uuid.UUID, name, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, timeoutSec int) (*poolEntry, error) {
	key := poolKey(tenantID, name)

	p.mu.Lock()
	if entry, ok := p.servers[key]; ok && entry.state.connected.Load() {
		entry.refCount++
		entry.lastUsed = time.Now()
		p.mu.Unlock()
		slog.Debug("mcp.pool.reuse", "key", key, "refCount", entry.refCount)
		return entry, nil
	}

	// If entry exists but disconnected, close old and reclaim slot
	if old, ok := p.servers[key]; ok {
		if old.state.cancel != nil {
			old.state.cancel()
		}
		if old.state.client != nil {
			_ = old.state.client.Close()
		}
		delete(p.servers, key)
		// Return slot to semaphore
		select {
		case <-p.slot:
		default:
		}
	}
	p.mu.Unlock()

	// Acquire a slot (blocks if pool full, evicts idle if possible)
	if err := p.acquireSlot(ctx); err != nil {
		return nil, fmt.Errorf("mcp pool exhausted: %w", err)
	}

	// Connect outside the lock (may be slow)
	ss, mcpTools, err := connectAndDiscover(ctx, name, transportType, command, args, env, url, headers, timeoutSec)
	if err != nil {
		// Return slot on failure
		select {
		case <-p.slot:
		default:
		}
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
		lastUsed: time.Now(),
	}

	p.mu.Lock()
	// Check if another goroutine connected while we were connecting
	if existing, ok := p.servers[key]; ok && existing.state.connected.Load() {
		p.mu.Unlock()
		hcancel()
		_ = ss.client.Close()
		// Return our extra slot
		select {
		case <-p.slot:
		default:
		}
		p.mu.Lock()
		existing.refCount++
		existing.lastUsed = time.Now()
		p.mu.Unlock()
		return existing, nil
	}
	p.servers[key] = entry
	p.mu.Unlock()

	slog.Info("mcp.pool.connected", "key", key, "tools", len(mcpTools))
	return entry, nil
}

// acquireSlot tries to acquire a pool slot, evicting idle connections if needed.
func (p *Pool) acquireSlot(ctx context.Context) error {
	// Fast path: slot available
	select {
	case p.slot <- struct{}{}:
		return nil
	default:
	}

	// Try evicting one idle entry
	p.mu.Lock()
	evicted := p.evictOldestIdleLocked()
	p.mu.Unlock()

	if evicted {
		select {
		case p.slot <- struct{}{}:
			return nil
		default:
		}
	}

	// Wait up to AcquireTimeout
	timer := time.NewTimer(p.cfg.AcquireTimeout)
	defer timer.Stop()

	select {
	case p.slot <- struct{}{}:
		return nil
	case <-timer.C:
		return fmt.Errorf("timeout after %s waiting for pool slot (max %d)", p.cfg.AcquireTimeout, p.cfg.MaxSize)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release decrements the reference count for a server.
// Accepts the same key format as Acquire (tenantID + name).
func (p *Pool) Release(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.servers[key]; ok {
		entry.refCount--
		if entry.refCount < 0 {
			entry.refCount = 0
		}
		entry.lastUsed = time.Now()
		slog.Debug("mcp.pool.release", "key", key, "refCount", entry.refCount)
	}
}

// Stop closes all pooled connections and stops eviction. Called on gateway shutdown.
func (p *Pool) Stop() {
	close(p.stopCh)

	p.mu.Lock()
	defer p.mu.Unlock()

	for key, entry := range p.servers {
		if entry.state.cancel != nil {
			entry.state.cancel()
		}
		if entry.state.client != nil {
			_ = entry.state.client.Close()
		}
		slog.Debug("mcp.pool.stopped", "key", key)
	}
	p.servers = make(map[string]*poolEntry)
}

// Evict closes a specific pooled connection by tenant + server name.
// Called when server credentials are rotated to force reconnection with new credentials.
func (p *Pool) Evict(tenantID uuid.UUID, serverName string) {
	key := poolKey(tenantID, serverName)
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.servers[key]
	if !ok {
		return
	}
	if entry.state.cancel != nil {
		entry.state.cancel()
	}
	if entry.state.client != nil {
		_ = entry.state.client.Close()
	}
	delete(p.servers, key)
	select {
	case <-p.slot:
	default:
	}
	slog.Info("mcp.pool.evicted_on_rotation", "key", key)
}

// evictLoop runs periodically to close idle connections over MaxIdle count.
func (p *Pool) evictLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.evictIdle()
		}
	}
}

// evictIdle closes connections idle > IdleTTL when total idle exceeds MaxIdle.
func (p *Pool) evictIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var idleKeys []string
	for key, entry := range p.servers {
		if entry.refCount == 0 && now.Sub(entry.lastUsed) > p.cfg.IdleTTL {
			idleKeys = append(idleKeys, key)
		}
	}

	// Count total idle (refCount == 0)
	totalIdle := 0
	for _, entry := range p.servers {
		if entry.refCount == 0 {
			totalIdle++
		}
	}

	// Only evict if over MaxIdle
	toEvict := totalIdle - p.cfg.MaxIdle
	if toEvict <= 0 && len(idleKeys) == 0 {
		return
	}

	// Evict TTL-expired first, then oldest if still over MaxIdle
	for _, key := range idleKeys {
		entry := p.servers[key]
		if entry.state.cancel != nil {
			entry.state.cancel()
		}
		if entry.state.client != nil {
			_ = entry.state.client.Close()
		}
		delete(p.servers, key)
		// Return slot
		select {
		case <-p.slot:
		default:
		}
		slog.Debug("mcp.pool.evicted", "key", key, "reason", "idle_ttl")
	}
}

// evictOldestIdleLocked evicts one idle entry (oldest lastUsed). Caller must hold mu.
func (p *Pool) evictOldestIdleLocked() bool {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range p.servers {
		if entry.refCount == 0 {
			if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.lastUsed
			}
		}
	}

	if oldestKey == "" {
		return false
	}

	entry := p.servers[oldestKey]
	if entry.state.cancel != nil {
		entry.state.cancel()
	}
	if entry.state.client != nil {
		_ = entry.state.client.Close()
	}
	delete(p.servers, oldestKey)
	// Return slot to semaphore
	select {
	case <-p.slot:
	default:
	}
	slog.Debug("mcp.pool.evicted", "key", oldestKey, "reason", "make_room")
	return true
}

// poolHealthLoop is a standalone health loop for pool-managed connections.
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
