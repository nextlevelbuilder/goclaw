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
			userID := store.UserIDFromContext(ctx)
			channel := ToolChannelFromCtx(ctx)
			if err := checkTeamAccess(ce.team.Settings, userID, channel); err != nil {
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

	// Check access
	userID := store.UserIDFromContext(ctx)
	channel := ToolChannelFromCtx(ctx)
	if err := checkTeamAccess(team.Settings, userID, channel); err != nil {
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
	m.agentKeyCache.Store(ag.AgentKey, e)
	return ag, nil
}

// cachedGetAgentByKey returns agent data from cache or DB with TTL.
func (m *TeamToolManager) cachedGetAgentByKey(ctx context.Context, key string) (*store.AgentData, error) {
	if entry, ok := m.agentKeyCache.Load(key); ok {
		ce := entry.(*agentCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			return ce.agent, nil
		}
		m.agentKeyCache.Delete(key)
	}
	ag, err := m.agentStore.GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := &agentCacheEntry{agent: ag, cachedAt: now}
	m.agentKeyCache.Store(key, e)
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

// resolveAgentByKey looks up an agent by key and returns its UUID.
func (m *TeamToolManager) resolveAgentByKey(key string) (uuid.UUID, error) {
	ag, err := m.cachedGetAgentByKey(context.Background(), key)
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

// taskTeamWorkspace extracts the team_workspace path from task metadata.
func taskTeamWorkspace(task *store.TeamTaskData) string {
	if task.Metadata == nil {
		return ""
	}
	ws, _ := task.Metadata["team_workspace"].(string)
	return ws
}
