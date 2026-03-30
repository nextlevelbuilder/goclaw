package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ResolverFunc is called when an agent isn't found in the cache.
// Used to lazy-create agents from DB. Context carries tenant scope.
type ResolverFunc func(ctx context.Context, agentKey string) (Agent, error)

const defaultRouterTTL = 10 * time.Minute

// agentEntry wraps a cached Agent with a timestamp for TTL-based expiration.
type agentEntry struct {
	agent    Agent
	cachedAt time.Time
}

// AgentActivityStatus tracks the current phase of a running agent for status queries.
type AgentActivityStatus struct {
	RunID     string
	Phase     string // "thinking", "tool_exec", "compacting"
	Tool      string // current tool name (when Phase == "tool_exec")
	Iteration int
	StartedAt time.Time
}

// Router manages multiple agent Loop instances.
// Each agent has a unique ID and its own provider/model/tools config.
// Cached Loops expire after TTL (safety net for multi-instance).
type Router struct {
	agents        map[string]*agentEntry
	mu            sync.RWMutex
	activeRuns    sync.Map     // runID → *ActiveRun
	sessionRuns   sync.Map     // sessionKey → runID (secondary index for O(1) IsSessionBusy)
	agentActivity sync.Map     // sessionKey → *AgentActivityStatus
	resolver      ResolverFunc // optional: lazy creation from DB
	ttl           time.Duration
}

func NewRouter() *Router {
	return &Router{
		agents: make(map[string]*agentEntry),
		ttl:    defaultRouterTTL,
	}
}

// SetResolver sets a resolver function for lazy agent creation.
func (r *Router) SetResolver(fn ResolverFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolver = fn
}

// Register adds an agent to the router.
func (r *Router) Register(ag Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[ag.ID()] = &agentEntry{agent: ag, cachedAt: time.Now()}
}

// Get returns an agent by ID. Lazy-creates from DB via resolver if needed.
// Cached entries expire after TTL as a safety net for multi-instance deployments.
// Cache key includes tenant so the same agent_key in different tenants resolves independently.
func (r *Router) Get(ctx context.Context, agentID string) (Agent, error) {
	cacheKey := agentCacheKey(ctx, agentID)

	r.mu.RLock()
	entry, ok := r.agents[cacheKey]
	resolver := r.resolver
	r.mu.RUnlock()

	if ok && (r.ttl == 0 || time.Since(entry.cachedAt) < r.ttl) {
		return entry.agent, nil
	}

	// TTL expired → remove stale entry so resolver re-creates
	if ok {
		r.mu.Lock()
		delete(r.agents, cacheKey)
		r.mu.Unlock()
	}

	// Try resolver (create from DB)
	if resolver != nil {
		ag, err := resolver(ctx, agentID)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		// Double-check: another goroutine might have created it
		if existing, ok := r.agents[cacheKey]; ok {
			r.mu.Unlock()
			return existing.agent, nil
		}
		r.agents[cacheKey] = &agentEntry{agent: ag, cachedAt: time.Now()}
		r.mu.Unlock()
		return ag, nil
	}

	return nil, fmt.Errorf("agent not found: %s", agentID)
}

// agentCacheKey builds a tenant-and-project-scoped cache key for the agent router.
// Different projects get separate cached Loop instances (each with project-specific MCP env).
func agentCacheKey(ctx context.Context, agentID string) string {
	key := agentID
	tid := store.TenantIDFromContext(ctx)
	if tid != uuid.Nil {
		key = tid.String() + ":" + key
	}
	if pid := store.ProjectIDFromContext(ctx); pid != "" {
		key += ":project:" + pid
	}
	return key
}

// Remove removes an agent from the router.
// Matches both plain and tenant-scoped keys (same as InvalidateAgent).
func (r *Router) Remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	suffix := ":" + agentID
	for key := range r.agents {
		if key == agentID || strings.HasSuffix(key, suffix) {
			delete(r.agents, key)
		}
	}
}

// List returns all registered agent IDs.
func (r *Router) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	return ids
}

// AgentInfo is lightweight metadata about an agent.
type AgentInfo struct {
	ID        string `json:"id"`
	Model     string `json:"model"`
	IsRunning bool   `json:"isRunning"`
}

// ListInfo returns metadata for all agents.
func (r *Router) ListInfo() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]AgentInfo, 0, len(r.agents))
	for _, entry := range r.agents {
		infos = append(infos, AgentInfo{
			ID:        entry.agent.ID(),
			Model:     entry.agent.Model(),
			IsRunning: entry.agent.IsRunning(),
		})
	}
	return infos
}

// IsRunning checks if a specific agent is currently running (cached in router).
func (r *Router) IsRunning(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.agents[agentID]; ok {
		return entry.agent.IsRunning()
	}
	return false
}

// --- Active Run Tracking (matching TS chat-abort.ts) ---

// ActiveRun tracks a running agent invocation so it can be aborted via chat.abort
// and supports mid-run message injection via InjectCh.
type ActiveRun struct {
	RunID      string
	SessionKey string
	AgentID    string
	Cancel     context.CancelFunc
	StartedAt  time.Time
	InjectCh   chan InjectedMessage // buffered channel for mid-run user message injection
}

// RegisterRun records an active run so it can be aborted later.
// Returns a receive-only channel for mid-run message injection.
func (r *Router) RegisterRun(runID, sessionKey, agentID string, cancel context.CancelFunc) <-chan InjectedMessage {
	injectCh := make(chan InjectedMessage, injectBufferSize)
	r.activeRuns.Store(runID, &ActiveRun{
		RunID:      runID,
		SessionKey: sessionKey,
		AgentID:    agentID,
		Cancel:     cancel,
		StartedAt:  time.Now(),
		InjectCh:   injectCh,
	})
	r.sessionRuns.Store(sessionKey, runID)
	return injectCh
}

// UnregisterRun removes a completed/cancelled run from tracking.
func (r *Router) UnregisterRun(runID string) {
	if val, ok := r.activeRuns.Load(runID); ok {
		run := val.(*ActiveRun)
		r.sessionRuns.Delete(run.SessionKey)
	}
	r.activeRuns.Delete(runID)
}

// AbortRun cancels a single run by ID. sessionKey is validated for authorization
// (matching TS chat-abort.ts: verify sessionKey matches before aborting).
// Returns true if the run was found and cancelled.
func (r *Router) AbortRun(runID, sessionKey string) bool {
	val, ok := r.activeRuns.Load(runID)
	if !ok {
		return false
	}
	run := val.(*ActiveRun)

	// Authorization: sessionKey must match (matching TS behavior)
	if sessionKey != "" && run.SessionKey != sessionKey {
		return false
	}

	run.Cancel()
	r.sessionRuns.Delete(run.SessionKey)
	r.activeRuns.Delete(runID)
	return true
}

// InjectMessage sends a user message to the running loop for a session.
// Returns true if the message was accepted, false if no active run or channel full.
func (r *Router) InjectMessage(sessionKey string, msg InjectedMessage) bool {
	runIDVal, ok := r.sessionRuns.Load(sessionKey)
	if !ok {
		return false
	}
	runVal, ok := r.activeRuns.Load(runIDVal)
	if !ok {
		return false
	}
	run := runVal.(*ActiveRun)
	select {
	case run.InjectCh <- msg:
		return true
	default:
		return false // channel full
	}
}

// InvalidateUserWorkspace clears the cached workspace for a user across all cached agent loops.
// Used when user_agent_profiles.workspace changes (e.g. admin reassignment).
func (r *Router) InvalidateUserWorkspace(userID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.agents {
		if loop, ok := entry.agent.(*Loop); ok {
			loop.InvalidateUserWorkspace(userID)
		}
	}
}

// SessionKeyForRun returns the session key associated with a run ID, or "" if not found.
func (r *Router) SessionKeyForRun(runID string) string {
	val, ok := r.activeRuns.Load(runID)
	if !ok {
		return ""
	}
	return val.(*ActiveRun).SessionKey
}

// UpdateActivity records the current phase of a running agent for status queries.
// Called from the bus subscriber on agent.activity events.
func (r *Router) UpdateActivity(sessionKey, runID, phase, tool string, iteration int) {
	r.agentActivity.Store(sessionKey, &AgentActivityStatus{
		RunID:     runID,
		Phase:     phase,
		Tool:      tool,
		Iteration: iteration,
		StartedAt: time.Now(),
	})
}

// ClearActivity removes the activity status for a session (on run completion).
func (r *Router) ClearActivity(sessionKey string) {
	r.agentActivity.Delete(sessionKey)
}

// GetActivity returns the current activity status for a session, or nil if idle.
func (r *Router) GetActivity(sessionKey string) *AgentActivityStatus {
	val, ok := r.agentActivity.Load(sessionKey)
	if !ok {
		return nil
	}
	return val.(*AgentActivityStatus)
}

// IsSessionBusy returns true if there's an active run for the given session key.
// O(1) via sessionRuns secondary index.
func (r *Router) IsSessionBusy(sessionKey string) bool {
	_, ok := r.sessionRuns.Load(sessionKey)
	return ok
}

// AbortRunsForSession cancels all active runs for a session key.
// Returns the list of aborted run IDs.
func (r *Router) AbortRunsForSession(sessionKey string) []string {
	var aborted []string
	r.activeRuns.Range(func(key, val any) bool {
		run := val.(*ActiveRun)
		if run.SessionKey == sessionKey {
			run.Cancel()
			r.activeRuns.Delete(key)
			r.sessionRuns.Delete(sessionKey)
			aborted = append(aborted, run.RunID)
		}
		return true
	})
	return aborted
}
