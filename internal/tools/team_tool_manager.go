package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workflowactions"
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
	teamStore           store.TeamStore
	agentStore          store.AgentStore
	msgBus              *bus.MessageBus
	dataDir             string   // base data directory for workspace path resolution
	teamCache           sync.Map // agentID (uuid.UUID) → *teamCacheEntry
	agentCache          sync.Map // agentID (uuid.UUID) → *agentCacheEntry
	agentKeyCache       sync.Map // agentKey (string) → *agentCacheEntry
	workflowRevalidator func(context.Context, *store.TeamWorkflowData) error
	workflowReplanner   workflowactions.ReplanFunc
	workflowActions     *workflowactions.Service
}

// Compatibility aliases keep tool backends source-compatible while the shared
// recovery service owns the neutral request and function types.
type WorkflowReplanRequest = workflowactions.ReplanRequest
type WorkflowReplanFunc = workflowactions.ReplanFunc

func (m *TeamToolManager) SetWorkflowRevalidator(revalidator func(context.Context, *store.TeamWorkflowData) error) {
	m.workflowRevalidator = revalidator
}

func (m *TeamToolManager) SetWorkflowReplanner(replanner WorkflowReplanFunc) {
	m.workflowReplanner = replanner
}

func (m *TeamToolManager) SetWorkflowActionService(service *workflowactions.Service) {
	m.workflowActions = service
}

func (m *TeamToolManager) WorkflowActionService() *workflowactions.Service {
	return m.workflowActions
}

func (m *TeamToolManager) ApplyWorkflowReplan(ctx context.Context, request WorkflowReplanRequest) (store.WorkflowActionResult, error) {
	if m.workflowReplanner == nil {
		return store.WorkflowActionResult{}, fmt.Errorf("workflow replanner is unavailable")
	}
	return m.workflowReplanner(ctx, request)
}

func (m *TeamToolManager) RevalidateWorkflow(ctx context.Context, workflow *store.TeamWorkflowData) error {
	if m.workflowRevalidator == nil {
		return fmt.Errorf("workflow revalidator is unavailable")
	}
	return m.workflowRevalidator(ctx, workflow)
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

func (m *TeamToolManager) Store() store.TeamStore { return m.teamStore }
func (m *TeamToolManager) DataDir() string        { return m.dataDir }
func (m *TeamToolManager) TryPublishInbound(msg bus.InboundMessage) bool {
	if m.msgBus == nil {
		return false
	}
	return m.msgBus.TryPublishInbound(msg)
}
