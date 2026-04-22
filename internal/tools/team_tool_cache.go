package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolveTeam returns the team that the calling agent belongs to.
// When ToolTeamIDFromCtx is set (task dispatch), uses that team ID directly
// instead of GetTeamForAgent — prevents wrong team resolution for multi-team agents.
// Uses a TTL cache to avoid repeated DB queries. Access control
// (user/channel) is checked on every call regardless of cache hit.
func (m *TeamToolManager) resolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error) {
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("no agent context — team tools require database stores")
	}

	// If team ID is explicitly set in context (from task dispatch), use it directly.
	// This prevents wrong team resolution when an agent belongs to multiple teams.
	if teamIDStr := ToolTeamIDFromCtx(ctx); teamIDStr != "" {
		teamUUID, err := uuid.Parse(teamIDStr)
		if err == nil && teamUUID != uuid.Nil {
			team, err := m.teamStore.GetTeam(ctx, teamUUID)
			if err != nil {
				slog.Warn("workspace: resolveTeam by context ID failed", "team_id", teamIDStr, "error", err)
				// Fall through to normal resolution
			} else if team != nil {
				return team, agentID, nil
			}
		}
	}

	// Check cache first
	if entry, ok := m.teamCache.Load(agentID); ok {
		ce := entry.(*teamCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			// Cache hit — still check access (user/channel vary per call)
			actorID := store.ActorIDFromContext(ctx)
			channel := ToolChannelFromCtx(ctx)
			if err := checkTeamAccess(ce.team.Settings, actorID, channel); err != nil {
				return nil, uuid.Nil, err
			}
			return ce.team, agentID, nil
		}
		m.teamCache.Delete(agentID) // expired
	}

	// Cache miss → DB
	team, err := m.teamStore.GetTeamForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("workspace: resolveTeam DB error", "agent_id", agentID, "error", err)
		return nil, uuid.Nil, fmt.Errorf("failed to resolve team: %w", err)
	}
	if team == nil {
		slog.Warn("workspace: agent has no team", "agent_id", agentID)
		return nil, uuid.Nil, fmt.Errorf("this agent is not part of any team")
	}

	// Store in cache (load members eagerly to avoid separate DB call later)
	members, _ := m.teamStore.ListMembers(ctx, team.ID)
	m.teamCache.Store(agentID, &teamCacheEntry{team: team, members: members, cachedAt: time.Now()})

	// Check access (ACTOR = real sender, matches per-user allow/deny lists in group chats #915)
	actorID := store.ActorIDFromContext(ctx)
	channel := ToolChannelFromCtx(ctx)
	if err := checkTeamAccess(team.Settings, actorID, channel); err != nil {
		return nil, uuid.Nil, err
	}

	return team, agentID, nil
}

// requireLead checks if the calling agent is the team lead.
// Teammate/system channels bypass this check (they act on behalf of the lead).
func (m *TeamToolManager) requireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error {
	channel := ToolChannelFromCtx(ctx)
	if channel == ChannelTeammate || channel == ChannelSystem {
		return nil
	}
	if agentID != team.LeadAgentID {
		return fmt.Errorf("only the team lead can perform this action")
	}
	return nil
}

// InvalidateTeam clears all cached team + member data.
// Called when team membership, settings, or links change.
// Full clear is acceptable because team mutations are rare (admin-initiated).
func (m *TeamToolManager) InvalidateTeam() {
	m.teamCache.Range(func(k, _ any) bool { m.teamCache.Delete(k); return true })
}

// InvalidateAgentCache clears all cached agent data (by ID and by key).
// Called via pub/sub when agent data changes (update/delete).
func (m *TeamToolManager) InvalidateAgentCache() {
	m.agentCache.Range(func(k, _ any) bool { m.agentCache.Delete(k); return true })
	m.agentKeyCache.Range(func(k, _ any) bool { m.agentKeyCache.Delete(k); return true })
}

// agentKeyCacheKey builds a tenant-scoped cache key for agentKeyCache.
// Agent keys (e.g. "my-agent") are unique per-tenant, not globally,
// so the cache key must include tenant to prevent cross-tenant pollution.
func agentKeyCacheKey(ctx context.Context, key string) string {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return "0:" + key
	}
	return tid.String() + ":" + key
}

// cachedGetAgentByID returns agent data from cache or DB with TTL.
func (m *TeamToolManager) cachedGetAgentByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	if entry, ok := m.agentCache.Load(id); ok {
		ce := entry.(*agentCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			return ce.agent, nil
		}
		m.agentCache.Delete(id)
	}
	ag, err := m.agentStore.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := &agentCacheEntry{agent: ag, cachedAt: now}
	m.agentCache.Store(id, e)
	m.agentKeyCache.Store(agentKeyCacheKey(ctx, ag.AgentKey), e)
	return ag, nil
}

// cachedGetAgentByKey returns agent data from cache or DB with TTL.
func (m *TeamToolManager) cachedGetAgentByKey(ctx context.Context, key string) (*store.AgentData, error) {
	ck := agentKeyCacheKey(ctx, key)
	if entry, ok := m.agentKeyCache.Load(ck); ok {
		ce := entry.(*agentCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			return ce.agent, nil
		}
		m.agentKeyCache.Delete(ck)
	}
	ag, err := m.agentStore.GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := &agentCacheEntry{agent: ag, cachedAt: now}
	m.agentKeyCache.Store(ck, e)
	m.agentCache.Store(ag.ID, e)
	return ag, nil
}

// cachedListMembers returns members from the team cache if available, or falls back to DB.
func (m *TeamToolManager) cachedListMembers(ctx context.Context, teamID uuid.UUID, agentID uuid.UUID) ([]store.TeamMemberData, error) {
	if entry, ok := m.teamCache.Load(agentID); ok {
		ce := entry.(*teamCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL && ce.team.ID == teamID && ce.members != nil {
			return ce.members, nil
		}
	}
	return m.teamStore.ListMembers(ctx, teamID)
}

// preWarmAgentKeyCache batch-fetches agents by key and populates the cache.
// Reduces N+1 queries when rendering task lists with agent display names.
func (m *TeamToolManager) preWarmAgentKeyCache(ctx context.Context, keys []string) {
	// Deduplicate and filter already-cached keys.
	unique := make(map[string]bool)
	var missing []string
	for _, k := range keys {
		if k == "" || unique[k] {
			continue
		}
		unique[k] = true
		ck := agentKeyCacheKey(ctx, k)
		if entry, ok := m.agentKeyCache.Load(ck); ok {
			ce := entry.(*agentCacheEntry)
			if time.Since(ce.cachedAt) < teamCacheTTL {
				continue // still valid
			}
		}
		missing = append(missing, k)
	}
	if len(missing) == 0 {
		return
	}

	agents, err := m.agentStore.GetByKeys(ctx, missing)
	if err != nil {
		slog.Debug("preWarmAgentKeyCache: batch fetch failed", "error", err)
		return
	}
	now := time.Now()
	for i := range agents {
		ag := &agents[i]
		e := &agentCacheEntry{agent: ag, cachedAt: now}
		ck := agentKeyCacheKey(ctx, ag.AgentKey)
		m.agentKeyCache.Store(ck, e)
		m.agentCache.Store(ag.ID, e)
	}
}

// preWarmAgentIDCache batch-fetches agents by UUID and populates the cache.
// Reduces N+1 queries when rendering task comments with agent keys.
func (m *TeamToolManager) preWarmAgentIDCache(ctx context.Context, ids []uuid.UUID) {
	// Deduplicate and filter already-cached IDs.
	seen := make(map[uuid.UUID]bool)
	var missing []uuid.UUID
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		if entry, ok := m.agentCache.Load(id); ok {
			ce := entry.(*agentCacheEntry)
			if time.Since(ce.cachedAt) < teamCacheTTL {
				continue
			}
		}
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return
	}

	agents, err := m.agentStore.GetByIDs(ctx, missing)
	if err != nil {
		slog.Debug("preWarmAgentIDCache: batch fetch failed", "error", err)
		return
	}
	now := time.Now()
	for i := range agents {
		ag := &agents[i]
		e := &agentCacheEntry{agent: ag, cachedAt: now}
		m.agentCache.Store(ag.ID, e)
		ck := agentKeyCacheKey(ctx, ag.AgentKey)
		m.agentKeyCache.Store(ck, e)
	}
}

// resolveAgentByKey looks up an agent by key and returns its UUID.
// Uses the caller's tenant-scoped context for isolation.
func (m *TeamToolManager) resolveAgentByKey(ctx context.Context, key string) (uuid.UUID, error) {
	ag, err := m.cachedGetAgentByKey(ctx, key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return ag.ID, nil
}

// agentKeyFromID returns the agent_key for a given UUID.
func (m *TeamToolManager) agentKeyFromID(ctx context.Context, id uuid.UUID) string {
	ag, err := m.cachedGetAgentByID(ctx, id)
	if err != nil {
		return id.String()
	}
	return ag.AgentKey
}

// ============================================================
// TeamToolBackend exported wrappers (cache layer)
// ============================================================

func (m *TeamToolManager) ResolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error) {
	return m.resolveTeam(ctx)
}
func (m *TeamToolManager) RequireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error {
	return m.requireLead(ctx, team, agentID)
}
func (m *TeamToolManager) ResolveAgentByKey(ctx context.Context, key string) (uuid.UUID, error) {
	return m.resolveAgentByKey(ctx, key)
}
func (m *TeamToolManager) AgentKeyFromID(ctx context.Context, id uuid.UUID) string {
	return m.agentKeyFromID(ctx, id)
}
func (m *TeamToolManager) CachedListMembers(ctx context.Context, teamID, agentID uuid.UUID) ([]store.TeamMemberData, error) {
	return m.cachedListMembers(ctx, teamID, agentID)
}
func (m *TeamToolManager) CachedGetAgentByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	return m.cachedGetAgentByID(ctx, id)
}
func (m *TeamToolManager) PreWarmAgentKeyCache(ctx context.Context, keys []string) {
	m.preWarmAgentKeyCache(ctx, keys)
}
func (m *TeamToolManager) PreWarmAgentIDCache(ctx context.Context, ids []uuid.UUID) {
	m.preWarmAgentIDCache(ctx, ids)
}

// taskTeamWorkspace extracts the team_workspace path from task metadata.
func taskTeamWorkspace(task *store.TeamTaskData) string {
	if task.Metadata == nil {
		return ""
	}
	ws, _ := task.Metadata[TaskMetaTeamWorkspace].(string)
	return ws
}
