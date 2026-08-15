package tools

import (
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const teamCacheTTL = 5 * time.Minute

// teamCacheEntry wraps cached team data + members with a timestamp for TTL expiration.
type teamCacheEntry struct {
	team     *store.TeamData
	members  []store.TeamMemberData // loaded together with team to avoid separate DB call
	cachedAt time.Time
}

// agentCacheEntry wraps cached agent data with a timestamp for TTL expiration.
type agentCacheEntry struct {
	agent    *store.AgentData
	cachedAt time.Time
}

// TeamToolManager is the shared backend for team_tasks tool and workspace interceptor.
// It resolves the calling agent's team from context and provides access to
// the team store, agent store, and message bus.
// Includes a TTL cache for team data to avoid DB queries on every tool call.
type TeamToolManager struct {
	teamStore     store.TeamStore
	agentStore    store.AgentStore
	msgBus        *bus.MessageBus
	dataDir       string   // base data directory for workspace path resolution
	teamCache     sync.Map // agentID (uuid.UUID) → *teamCacheEntry
	agentCache    sync.Map // agentID (uuid.UUID) → *agentCacheEntry
	agentKeyCache sync.Map // agentKey (string) → *agentCacheEntry
}

func NewTeamToolManager(teamStore store.TeamStore, agentStore store.AgentStore, msgBus *bus.MessageBus, dataDir string) *TeamToolManager {
	return &TeamToolManager{teamStore: teamStore, agentStore: agentStore, msgBus: msgBus, dataDir: dataDir}
}

// ============================================================
// TeamToolBackend exported wrappers
// These thin wrappers satisfy the TeamToolBackend interface
// while keeping the unexported originals for internal use
// (WorkspaceInterceptor, PostTurnProcessor, etc.).
// ============================================================

func (m *TeamToolManager) Store() store.TeamStore                { return m.teamStore }
func (m *TeamToolManager) DataDir() string                       { return m.dataDir }
func (m *TeamToolManager) TryPublishInbound(msg bus.InboundMessage) bool {
	if m.msgBus == nil {
		return false
	}
	return m.msgBus.TryPublishInbound(msg)
}
