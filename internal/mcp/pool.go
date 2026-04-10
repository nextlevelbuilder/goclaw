package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// PoolConfig configures the MCP connection pool.
type PoolConfig struct {
	MaxSize            int           // global max connections (default 200)
	MaxIdle            int           // max idle connections to keep alive (default 20)
	IdleTTL            time.Duration // close idle connections after this (default 20m)
	AcquireTimeout     time.Duration // wait for pool slot before error (default 60s)
	MaxUserConns       int           // max per-user connections per MCP server (default 30)
	UserIdleTTL        time.Duration // close idle user connections after this (default 15m)
	UserAcquireTimeout time.Duration // wait for user pool slot before error (default 10s)
}

// DefaultPoolConfig returns the default pool configuration.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxSize:            200,
		MaxIdle:            20,
		IdleTTL:            20 * time.Minute,
		AcquireTimeout:     60 * time.Second,
		MaxUserConns:       30,
		UserIdleTTL:        15 * time.Minute,
		UserAcquireTimeout: 10 * time.Second,
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
// Per-user connections are keyed by tenantID/serverName/user:userID.
type Pool struct {
	mu          sync.Mutex
	servers     map[string]*poolEntry            // shared connections: tenantID/serverName
	userServers map[string]*poolEntry            // user connections: tenantID/serverName/user:userID
	userSlots   map[string]chan struct{}          // per-server semaphores: tenantID/serverName → capacity MaxUserConns
	cfg         PoolConfig
	slot        chan struct{} // semaphore for MaxSize
	stopCh      chan struct{}
}

// NewPool creates a shared MCP connection pool with idle eviction.
func NewPool(cfg PoolConfig) *Pool {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 200
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
	if cfg.MaxUserConns <= 0 {
		cfg.MaxUserConns = 30
	}
	if cfg.UserIdleTTL <= 0 {
		cfg.UserIdleTTL = 15 * time.Minute
	}
	if cfg.UserAcquireTimeout <= 0 {
		cfg.UserAcquireTimeout = 10 * time.Second
	}

	p := &Pool{
		servers:     make(map[string]*poolEntry),
		userServers: make(map[string]*poolEntry),
		userSlots:   make(map[string]chan struct{}),
		cfg:         cfg,
		slot:        make(chan struct{}, cfg.MaxSize),
		stopCh:      make(chan struct{}),
	}
	go p.evictLoop()
	return p
}

// poolKey builds a tenant-scoped key for pool lookups.
func poolKey(tenantID uuid.UUID, name string) string {
	return tenantID.String() + "/" + name
}

// UserPoolKey builds a tenant+user-scoped key for user pool lookups.
// Exported for callers that need to construct release keys.
func UserPoolKey(tenantID uuid.UUID, serverName, userID string) string {
	return tenantID.String() + "/" + serverName + "/user:" + userID
}

// userSlotKey returns the per-server semaphore key (tenantID/serverName).
func userSlotKey(tenantID uuid.UUID, serverName string) string {
	return tenantID.String() + "/" + serverName
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
		if client := old.state.clientPtr.Load(); client != nil {
			_ = client.Close()
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

// AcquireUser returns a per-user connection for the named server scoped to a tenant+user.
// If no connection exists, it connects using the provided config.
// Blocks up to UserAcquireTimeout if per-server user slot limit is reached.
func (p *Pool) AcquireUser(ctx context.Context, tenantID uuid.UUID, name, userID, transportType, command string, args []string, env map[string]string, url string, headers map[string]string, timeoutSec int) (*poolEntry, error) {
	key := UserPoolKey(tenantID, name, userID)
	slotKey := userSlotKey(tenantID, name)

	p.mu.Lock()
	if entry, ok := p.userServers[key]; ok && entry.state.connected.Load() {
		entry.refCount++
		entry.lastUsed = time.Now()
		p.mu.Unlock()
		slog.Debug("mcp.pool.user.reuse", "key", key, "refCount", entry.refCount)
		return entry, nil
	}

	// If entry exists but disconnected, close old and reclaim slot
	if old, ok := p.userServers[key]; ok {
		if old.state.cancel != nil {
			old.state.cancel()
		}
		if client := old.state.clientPtr.Load(); client != nil {
			_ = client.Close()
		}
		delete(p.userServers, key)
		// Return slot to per-server semaphore
		if sem, ok := p.userSlots[slotKey]; ok {
			select {
			case <-sem:
			default:
			}
		}
	}

	// Ensure per-server semaphore exists (lazy init under lock)
	if _, ok := p.userSlots[slotKey]; !ok {
		p.userSlots[slotKey] = make(chan struct{}, p.cfg.MaxUserConns)
	}
	sem := p.userSlots[slotKey]
	p.mu.Unlock()

	// Acquire a user slot for this server (blocks up to UserAcquireTimeout)
	if err := p.acquireUserSlot(ctx, sem, slotKey); err != nil {
		return nil, fmt.Errorf("mcp user pool exhausted for server %s: %w", name, err)
	}

	// Connect outside the lock (may be slow)
	ss, mcpTools, err := connectAndDiscover(ctx, name, transportType, command, args, env, url, headers, timeoutSec)
	if err != nil {
		// Return slot on failure
		select {
		case <-sem:
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
	if existing, ok := p.userServers[key]; ok && existing.state.connected.Load() {
		p.mu.Unlock()
		hcancel()
		_ = ss.client.Close()
		// Return our extra slot
		select {
		case <-sem:
		default:
		}
		p.mu.Lock()
		existing.refCount++
		existing.lastUsed = time.Now()
		p.mu.Unlock()
		return existing, nil
	}
	p.userServers[key] = entry
	p.mu.Unlock()

	slog.Info("mcp.pool.user.connected", "key", key, "tools", len(mcpTools))
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

// acquireUserSlot tries to acquire a per-server user slot.
func (p *Pool) acquireUserSlot(ctx context.Context, sem chan struct{}, slotKey string) error {
	// Fast path: slot available
	select {
	case sem <- struct{}{}:
		return nil
	default:
	}

	// Wait up to UserAcquireTimeout
	timer := time.NewTimer(p.cfg.UserAcquireTimeout)
	defer timer.Stop()

	select {
	case sem <- struct{}{}:
		return nil
	case <-timer.C:
		return fmt.Errorf("timeout after %s waiting for user slot (max %d, server %s)", p.cfg.UserAcquireTimeout, p.cfg.MaxUserConns, slotKey)
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

// ReleaseUser decrements the reference count for a user-scoped connection.
// Accepts the same key format as AcquireUser (tenantID + serverName + userID).
func (p *Pool) ReleaseUser(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.userServers[key]; ok {
		entry.refCount--
		if entry.refCount < 0 {
			entry.refCount = 0
		}
		entry.lastUsed = time.Now()
		slog.Debug("mcp.pool.user.release", "key", key, "refCount", entry.refCount)
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
		if client := entry.state.clientPtr.Load(); client != nil {
			_ = client.Close()
		}
		slog.Debug("mcp.pool.stopped", "key", key)
	}
	p.servers = make(map[string]*poolEntry)

	for key, entry := range p.userServers {
		if entry.state.cancel != nil {
			entry.state.cancel()
		}
		if client := entry.state.clientPtr.Load(); client != nil {
			_ = client.Close()
		}
		slog.Debug("mcp.pool.user.stopped", "key", key)
	}
	p.userServers = make(map[string]*poolEntry)
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
	if client := entry.state.clientPtr.Load(); client != nil {
		_ = client.Close()
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
// Also evicts user connections idle > UserIdleTTL.
func (p *Pool) evictIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	// Evict shared connections
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
	if toEvict > 0 || len(idleKeys) > 0 {
		for _, key := range idleKeys {
			entry := p.servers[key]
			if entry.state.cancel != nil {
				entry.state.cancel()
			}
			if client := entry.state.clientPtr.Load(); client != nil {
				_ = client.Close()
			}
			delete(p.servers, key)
			select {
			case <-p.slot:
			default:
			}
			slog.Debug("mcp.pool.evicted", "key", key, "reason", "idle_ttl")
		}
	}

	// Evict user connections idle > UserIdleTTL
	for key, entry := range p.userServers {
		if entry.refCount == 0 && now.Sub(entry.lastUsed) > p.cfg.UserIdleTTL {
			if entry.state.cancel != nil {
				entry.state.cancel()
			}
			if client := entry.state.clientPtr.Load(); client != nil {
				_ = client.Close()
			}
			delete(p.userServers, key)
			// Return slot to per-server semaphore
			// Extract slotKey from user key: "tenantID/serverName/user:userID" → "tenantID/serverName"
			// We search userSlots by iterating — key format guarantees prefix match
			for slotKey, sem := range p.userSlots {
				if strings.HasPrefix(key, slotKey+"/") {
					select {
					case <-sem:
					default:
					}
					break
				}
			}
			slog.Debug("mcp.pool.user.evicted", "key", key, "reason", "idle_ttl")
		}
	}
}

// evictOldestIdleLocked evicts one idle entry (oldest lastUsed) from shared or user pools.
// Caller must hold mu.
func (p *Pool) evictOldestIdleLocked() bool {
	var oldestKey string
	var oldestTime time.Time
	isUser := false

	for key, entry := range p.servers {
		if entry.refCount == 0 {
			if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.lastUsed
				isUser = false
			}
		}
	}

	for key, entry := range p.userServers {
		if entry.refCount == 0 {
			if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.lastUsed
				isUser = true
			}
		}
	}

	if oldestKey == "" {
		return false
	}

	if isUser {
		entry := p.userServers[oldestKey]
		if entry.state.cancel != nil {
			entry.state.cancel()
		}
		if entry.state.client != nil {
			_ = entry.state.client.Close()
		}
		delete(p.userServers, oldestKey)
		for slotKey, sem := range p.userSlots {
			if strings.HasPrefix(oldestKey, slotKey+"/") {
				select {
				case <-sem:
				default:
				}
				break
			}
		}
	} else {
		entry := p.servers[oldestKey]
		if entry.state.cancel != nil {
			entry.state.cancel()
		}
		if entry.state.client != nil {
			_ = entry.state.client.Close()
		}
		delete(p.servers, oldestKey)
		select {
		case <-p.slot:
		default:
		}
	}

	slog.Debug("mcp.pool.evicted", "key", oldestKey, "reason", "make_room", "user", isUser)
	return true
}

// ClientPtr returns the atomic client pointer for this pool entry.
// Used by BridgeTools to atomically load the current client during reconnect.
func (e *poolEntry) ClientPtr() *atomic.Pointer[mcpclient.Client] { return &e.state.clientPtr }

// Connected returns a pointer to the connected flag for this pool entry.
func (e *poolEntry) Connected() *atomic.Bool { return &e.state.connected }

// MCPTools returns the discovered MCP tool definitions for this pool entry.
func (e *poolEntry) MCPTools() []mcpgo.Tool { return e.tools }

// poolHealthLoop is a standalone health loop for pool-managed connections.
// After consecutive ping failures, it attempts a full reconnect by creating
// a fresh client, mirroring the Manager.tryReconnect slow path.
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
					ss.mu.Lock()
					ss.healthFailures = 0
					ss.mu.Unlock()
					continue
				}
				ss.mu.Lock()
				ss.healthFailures++
				failures := ss.healthFailures
				ss.lastErr = err.Error()
				ss.mu.Unlock()

				slog.Warn("mcp.pool.health_failed", "server", ss.name, "error", err, "consecutive", failures)

				if failures >= healthFailThreshold {
					ss.connected.Store(false)
					poolTryReconnect(ctx, ss)
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

// poolTryReconnect attempts reconnect for a pool-managed connection.
// Delegates to the shared reconnectWithBackoff with pool-specific log prefix.
func poolTryReconnect(ctx context.Context, ss *serverState) {
	reconnectWithBackoff(ctx, ss, "mcp.pool")
}
