package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ResolverFunc is called when an agent isn't found in the cache.
// Used to lazy-create agents from DB.
type ResolverFunc func(agentKey string) (Agent, error)

const defaultRouterTTL = 10 * time.Minute

// agentEntry wraps a cached Agent with a timestamp for TTL-based expiration.
type agentEntry struct {
	agent    Agent
	cachedAt time.Time
}

// Router manages multiple agent Loop instances.
// Each agent has a unique ID and its own provider/model/tools config.
// Cached Loops expire after TTL (safety net for multi-instance).
type Router struct {
	agents     map[string]*agentEntry
	mu         sync.RWMutex
	activeRuns sync.Map // runID → *ActiveRun
	resolver   ResolverFunc // optional: lazy creation from DB
	ttl        time.Duration
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
func (r *Router) Get(agentID string) (Agent, error) {
	r.mu.RLock()
	entry, ok := r.agents[agentID]
	resolver := r.resolver
	r.mu.RUnlock()

	if ok && (r.ttl == 0 || time.Since(entry.cachedAt) < r.ttl) {
		return entry.agent, nil
	}

	// TTL expired → remove stale entry so resolver re-creates
	if ok {
		r.mu.Lock()
		delete(r.agents, agentID)
		r.mu.Unlock()
	}

	// Try resolver (create from DB)
	if resolver != nil {
		ag, err := resolver(agentID)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		// Double-check: another goroutine might have created it
		if existing, ok := r.agents[agentID]; ok {
			r.mu.Unlock()
			return existing.agent, nil
		}
		r.agents[agentID] = &agentEntry{agent: ag, cachedAt: time.Now()}
		r.mu.Unlock()
		return ag, nil
	}

	return nil, fmt.Errorf("agent not found: %s", agentID)
}

// Remove removes an agent from the router.
func (r *Router) Remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, agentID)
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

// ActiveRun tracks a running agent invocation so it can be aborted via chat.abort.
type ActiveRun struct {
	RunID      string
	SessionKey string
	AgentID    string
	Cancel     context.CancelFunc
	StartedAt  time.Time
}

// RegisterRun records an active run so it can be aborted later.
func (r *Router) RegisterRun(runID, sessionKey, agentID string, cancel context.CancelFunc) {
	r.activeRuns.Store(runID, &ActiveRun{
		RunID:      runID,
		SessionKey: sessionKey,
		AgentID:    agentID,
		Cancel:     cancel,
		StartedAt:  time.Now(),
	})
}

// UnregisterRun removes a completed/cancelled run from tracking.
func (r *Router) UnregisterRun(runID string) {
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
	r.activeRuns.Delete(runID)
	return true
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

// AbortRunsForSession cancels all active runs for a session key.
// Returns the list of aborted run IDs.
func (r *Router) AbortRunsForSession(sessionKey string) []string {
	var aborted []string
	r.activeRuns.Range(func(key, val interface{}) bool {
		run := val.(*ActiveRun)
		if run.SessionKey == sessionKey {
			run.Cancel()
			r.activeRuns.Delete(key)
			aborted = append(aborted, run.RunID)
		}
		return true
	})
	return aborted
}
