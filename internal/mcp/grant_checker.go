package mcp

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// GrantChecker verifies if an agent/user still has access to an MCP server tool.
// Used by BridgeTool.Execute to recheck grants at runtime.
type GrantChecker interface {
	// IsAllowed returns (true, "") if the agent+user combination has access
	// to the specified serverID and toolName. On denial returns (false, reason)
	// where reason is a short machine-friendly tag suitable for logs and for
	// surfacing to operators in error messages. Possible reasons:
	//   - "server_not_accessible" — no agent grant, server disabled, or revoked
	//   - "tool_not_in_allow_list" — grant exists but tool not in tool_allow
	//   - "load_failed: <err>"     — store query failed (fail-closed)
	IsAllowed(ctx context.Context, agentID uuid.UUID, userID string, serverID uuid.UUID, toolName string) (bool, string)
}

// cacheKey identifies a unique agent+user combination for caching.
type cacheKey struct {
	agentID uuid.UUID
	userID  string
}

// cacheEntry holds the resolved access info for an agent+user.
type cacheEntry struct {
	// allowByServer maps serverID → toolAllowSet.
	// nil toolAllowSet means "all tools allowed" (no tool_allow filter).
	// non-nil empty map means "no tools allowed" (grant exists but tool_allow is empty).
	// Absent key means "server not accessible" (no grant).
	allowByServer map[uuid.UUID]map[string]bool
}

// storeGrantChecker implements GrantChecker backed by MCPServerStore.
// Uses sync.Map for caching with event-bus invalidation (no TTL).
type storeGrantChecker struct {
	store store.MCPServerStore
	cache sync.Map // map[cacheKey]*cacheEntry
}

// NewStoreGrantChecker creates a GrantChecker backed by the MCP store.
// Subscribes to TopicCacheMCP for cache invalidation on grant changes.
func NewStoreGrantChecker(mcpStore store.MCPServerStore, msgBus *bus.MessageBus) *storeGrantChecker {
	gc := &storeGrantChecker{
		store: mcpStore,
	}

	// Subscribe to cache invalidation events.
	// When MCP grants are revoked/modified, the HTTP handler broadcasts to this topic.
	if msgBus != nil {
		msgBus.Subscribe(bus.TopicCacheMCP, func(event bus.Event) {
			if event.Name == protocol.EventCacheInvalidate {
				gc.cache.Clear()
				slog.Debug("mcp.grant_checker.cache_cleared", "trigger", "bus_event")
			}
		})
	}

	return gc
}

// IsAllowed checks if the agent+user has access to the server and tool.
func (gc *storeGrantChecker) IsAllowed(ctx context.Context, agentID uuid.UUID, userID string, serverID uuid.UUID, toolName string) (bool, string) {
	if gc.store == nil {
		// No store = config-path mode, skip grant check
		return true, ""
	}

	key := cacheKey{agentID: agentID, userID: userID}

	// Try cache first
	if cached, ok := gc.cache.Load(key); ok {
		entry := cached.(*cacheEntry)
		return gc.checkAccess(entry, serverID, toolName, agentID, userID)
	}

	// Cache miss — query store
	entry, err := gc.loadEntry(ctx, agentID, userID)
	if err != nil {
		slog.Warn("mcp.grant_checker.load_failed", "agent", agentID, "user", userID, "error", err)
		// On error, deny access (fail-closed)
		return false, "load_failed: " + err.Error()
	}

	// Self-healing guard: an empty allowByServer means ListAccessible returned
	// zero rows. Caching that would pin permanent denial for this (agent,user)
	// until a bus invalidate fires — and if the emptiness came from a transient
	// condition (race during agent reload, momentary scope misroute, store
	// flake that returned nil rows instead of an error), the user stays locked
	// out indefinitely. Skipping the cache write here means the next call
	// re-queries the store, so the system recovers as soon as the condition
	// clears. The cost is one extra ListAccessible per call while truly empty,
	// which is acceptable because agents with zero MCP grants don't call MCP
	// tools in steady state.
	if len(entry.allowByServer) == 0 {
		slog.Debug("mcp.grant_checker.empty_no_cache", "agent", agentID, "user", userID)
		return gc.checkAccess(entry, serverID, toolName, agentID, userID)
	}

	gc.cache.Store(key, entry)
	return gc.checkAccess(entry, serverID, toolName, agentID, userID)
}

// loadEntry queries the store for accessible servers and builds a cache entry.
func (gc *storeGrantChecker) loadEntry(ctx context.Context, agentID uuid.UUID, userID string) (*cacheEntry, error) {
	accessible, err := gc.store.ListAccessible(ctx, agentID, userID)
	if err != nil {
		return nil, err
	}

	entry := &cacheEntry{
		allowByServer: make(map[uuid.UUID]map[string]bool, len(accessible)),
	}

	for _, info := range accessible {
		serverID := info.Server.ID

		// Build tool allow set from grant's tool_allow field
		if len(info.ToolAllow) == 0 {
			// No tool_allow filter = all tools allowed
			entry.allowByServer[serverID] = nil
		} else {
			// Build set from tool_allow list
			allowSet := make(map[string]bool, len(info.ToolAllow))
			for _, t := range info.ToolAllow {
				allowSet[t] = true
			}
			entry.allowByServer[serverID] = allowSet
		}
	}

	return entry, nil
}

// checkAccess evaluates if the entry allows access to serverID+toolName.
// Returns (allowed, reason). Reason is empty when allowed.
func (gc *storeGrantChecker) checkAccess(entry *cacheEntry, serverID uuid.UUID, toolName string, agentID uuid.UUID, userID string) (bool, string) {
	allowSet, hasServer := entry.allowByServer[serverID]
	if !hasServer {
		slog.Warn("security.mcp_grant_revoked_at_execute",
			"agent", agentID,
			"user", userID,
			"server", serverID,
			"tool", toolName,
			"reason", "server_not_accessible",
		)
		return false, "server_not_accessible"
	}

	// nil allowSet means "all tools allowed"
	if allowSet == nil {
		return true, ""
	}

	// Check if tool is in the allow set
	if !allowSet[toolName] {
		slog.Warn("security.mcp_grant_revoked_at_execute",
			"agent", agentID,
			"user", userID,
			"server", serverID,
			"tool", toolName,
			"reason", "tool_not_in_allow_list",
		)
		return false, "tool_not_in_allow_list"
	}

	return true, ""
}

// Invalidate clears the entire cache. Called when grants are modified.
func (gc *storeGrantChecker) Invalidate() {
	gc.cache.Clear()
}
