package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const teamCacheTTL = 5 * time.Minute

// teamCacheEntry wraps cached team data with a timestamp for TTL expiration.
type teamCacheEntry struct {
	team     *store.TeamData
	cachedAt time.Time
}

// TeamToolManager is the shared backend for team_tasks and team_message tools.
// It resolves the calling agent's team from context and provides access to
// the team store, agent store, and message bus.
// Includes a TTL cache for team data to avoid DB queries on every tool call.
type TeamToolManager struct {
	teamStore   store.TeamStore
	agentStore  store.AgentStore
	msgBus      *bus.MessageBus
	delegateMgr *DelegateManager // optional: enables delegation cancellation on task cancel
	teamCache   sync.Map         // agentID (uuid.UUID) → *teamCacheEntry
}

func NewTeamToolManager(teamStore store.TeamStore, agentStore store.AgentStore, msgBus *bus.MessageBus) *TeamToolManager {
	return &TeamToolManager{teamStore: teamStore, agentStore: agentStore, msgBus: msgBus}
}

// SetDelegateManager enables delegation cancellation when team tasks are cancelled.
func (m *TeamToolManager) SetDelegateManager(dm *DelegateManager) {
	m.delegateMgr = dm
}

// resolveTeam returns the team that the calling agent belongs to.
// Uses a TTL cache to avoid repeated DB queries. Access control
// (user/channel) is checked on every call regardless of cache hit.
func (m *TeamToolManager) resolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error) {
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("no agent context — team tools require database stores")
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
		return nil, uuid.Nil, fmt.Errorf("failed to resolve team: %w", err)
	}
	if team == nil {
		return nil, uuid.Nil, fmt.Errorf("this agent is not part of any team")
	}

	// Store in cache
	m.teamCache.Store(agentID, &teamCacheEntry{team: team, cachedAt: time.Now()})

	// Check access
	userID := store.UserIDFromContext(ctx)
	channel := ToolChannelFromCtx(ctx)
	if err := checkTeamAccess(team.Settings, userID, channel); err != nil {
		return nil, uuid.Nil, err
	}

	return team, agentID, nil
}

// requireLead checks if the calling agent is the team lead.
// Delegate/system channels bypass this check (they act on behalf of the lead).
func (m *TeamToolManager) requireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error {
	channel := ToolChannelFromCtx(ctx)
	if channel == "delegate" || channel == "system" {
		return nil
	}
	if agentID != team.LeadAgentID {
		return fmt.Errorf("only the team lead can perform this action")
	}
	return nil
}

// InvalidateTeam clears all cached team data.
// Called when team membership, settings, or links change.
// Full clear is acceptable because team mutations are rare (admin-initiated).
func (m *TeamToolManager) InvalidateTeam() {
	m.teamCache = sync.Map{}
}

// resolveAgentByKey looks up an agent by key and returns its UUID.
func (m *TeamToolManager) resolveAgentByKey(key string) (uuid.UUID, error) {
	ag, err := m.agentStore.GetByKey(context.Background(), key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return ag.ID, nil
}

// agentKeyFromID returns the agent_key for a given UUID.
func (m *TeamToolManager) agentKeyFromID(ctx context.Context, id uuid.UUID) string {
	ag, err := m.agentStore.GetByID(ctx, id)
	if err != nil {
		return id.String()
	}
	return ag.AgentKey
}

// broadcastTeamEvent sends a real-time event via the message bus for team activity visibility.
func (m *TeamToolManager) broadcastTeamEvent(name string, payload any) {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    name,
		Payload: payload,
	})
}

// agentDisplayName returns the display name for an agent key, falling back to empty string.
func (m *TeamToolManager) agentDisplayName(ctx context.Context, key string) string {
	ag, err := m.agentStore.GetByKey(ctx, key)
	if err != nil || ag.DisplayName == "" {
		return ""
	}
	return ag.DisplayName
}
