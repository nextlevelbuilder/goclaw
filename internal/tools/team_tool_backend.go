package tools

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TeamToolBackend abstracts the TeamToolManager for action handlers.
// This interface enables unit testing of action handlers with a mock backend.
// WorkspaceInterceptor and PostTurnProcessor continue to use *TeamToolManager directly.
type TeamToolBackend interface {
	// Team resolution & auth
	ResolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error)
	RequireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error

	// Store access
	Store() store.TeamStore

	// Agent resolution
	ResolveAgentByKey(ctx context.Context, key string) (uuid.UUID, error)
	AgentKeyFromID(ctx context.Context, id uuid.UUID) string
	AgentDisplayName(ctx context.Context, key string) string
	CachedListMembers(ctx context.Context, teamID, agentID uuid.UUID) ([]store.TeamMemberData, error)
	CachedGetAgentByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error)
	PreWarmAgentKeyCache(ctx context.Context, keys []string)
	PreWarmAgentIDCache(ctx context.Context, ids []uuid.UUID)

	// Side effects
	BroadcastTeamEvent(ctx context.Context, name string, payload any)
	DispatchTaskToAgent(ctx context.Context, task *store.TeamTaskData, team *store.TeamData, agentID uuid.UUID)
	TryPublishInbound(msg bus.InboundMessage) bool

	// Dispatch helpers
	BuildBlockerResultsSummary(ctx context.Context, task *store.TeamTaskData) string
	BuildRecentCommentsSummary(ctx context.Context, taskID uuid.UUID) string
	RestoreTraceContext(ctx context.Context, task *store.TeamTaskData) context.Context

	// Settings helpers
	FollowupDelayMinutes(team *store.TeamData) int
	FollowupMaxReminders(team *store.TeamData) int

	// Data directory for workspace resolution
	DataDir() string
}

// Compile-time check: *TeamToolManager must satisfy TeamToolBackend.
var _ TeamToolBackend = (*TeamToolManager)(nil)
