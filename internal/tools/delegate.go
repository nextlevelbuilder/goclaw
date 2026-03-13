package tools

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultMaxDelegationLoad = 5
const defaultProgressInterval = 30 * time.Second

// DelegationTask tracks an active delegation for concurrency control and cancellation.
type DelegationTask struct {
	ID             string     `json:"id"`
	SourceAgentID  uuid.UUID  `json:"source_agent_id"`
	SourceAgentKey string     `json:"source_agent_key"`
	TargetAgentID  uuid.UUID  `json:"target_agent_id"`
	SourceDisplayName  string `json:"-"`
	TargetAgentKey     string `json:"target_agent_key"`
	TargetDisplayName  string `json:"-"`
	UserID         string     `json:"user_id"`
	Task           string     `json:"task"`
	Status         string     `json:"status"` // "running", "completed", "failed", "cancelled"
	Mode           string     `json:"mode"`   // "sync" or "async"
	SessionKey     string     `json:"session_key"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`

	// Origin metadata for async announce routing
	OriginChannel    string `json:"-"`
	OriginChatID     string `json:"-"`
	OriginPeerKind   string `json:"-"`
	OriginLocalKey   string `json:"-"` // composite key with topic/thread suffix for routing
	OriginSessionKey string `json:"-"` // exact parent session key for announce routing (WS uses non-standard format)

	// Trace context for announce linking (same pattern as SubagentTask)
	OriginTraceID    uuid.UUID `json:"-"`
	OriginRootSpanID uuid.UUID `json:"-"`

	// Team tracking
	TeamID     uuid.UUID `json:"-"` // from link.TeamID (for delegation history)
	TeamTaskID uuid.UUID `json:"-"`

	// Activity tracking (updated via UpdateActivity on agent.activity events)
	LastActivity string       `json:"-"` // "thinking", "tool_exec", "compacting"
	LastTool     string       `json:"-"` // current tool name (when LastActivity == "tool_exec")
	activityMu   sync.RWMutex `json:"-"`

	cancelFunc      context.CancelFunc `json:"-"`
	progressEnabled bool               `json:"-"` // resolved from team settings or global default
}

// UpdateActivity sets the current phase and tool for this delegation.
func (t *DelegationTask) UpdateActivity(phase, tool string) {
	t.activityMu.Lock()
	t.LastActivity = phase
	t.LastTool = tool
	t.activityMu.Unlock()
}

// GetActivity returns the current phase and tool for this delegation.
func (t *DelegationTask) GetActivity() (phase, tool string) {
	t.activityMu.RLock()
	phase = t.LastActivity
	tool = t.LastTool
	t.activityMu.RUnlock()
	return
}

// originKey returns a composite key scoping this delegation to its origin conversation.
// Used for sibling counting and artifact accumulation so that delegations from
// different channels/chats are NOT treated as siblings of each other.
func (t *DelegationTask) originKey() string {
	return t.SourceAgentID.String() + ":" + t.OriginChannel + ":" + t.OriginChatID
}

// DelegateOpts configures a single delegation call.
type DelegateOpts struct {
	TargetAgentKey    string
	Task              string
	Context           string        // optional extra context
	Mode              string        // "sync" (default) or "async"
	TeamTaskID        uuid.UUID     // optional: auto-complete this team task on success
	EstimatedDuration time.Duration // optional: reserved for future use (progress uses periodic interval now)
	Label             string        // optional: short label for auto-created task subject (falls back to Task)
}

// DelegateRunRequest is the request passed to the AgentRunFunc callback.
// Mirrors agent.RunRequest without importing the agent package (avoids import cycle).
type DelegateRunRequest struct {
	SessionKey        string
	Message           string
	UserID            string
	Channel           string
	ChatID            string
	PeerKind          string
	RunID             string
	Stream            bool
	ExtraSystemPrompt string
	MaxIterations     int // per-delegation override (0 = use agent default)

	// Media propagation: parent's media files for delegate's vision context.
	Media []bus.MediaFile

	// Delegation context (bridged to agent.RunRequest for event enrichment)
	DelegationID  string
	TeamID        string
	TeamTaskID    string
	ParentAgentID string

	// Workspace scope propagation (set by delegation, read by workspace tools)
	WorkspaceChannel string
	WorkspaceChatID  string
}

// DelegateRunResult is the result from AgentRunFunc.
type DelegateRunResult struct {
	Content      string
	Iterations   int
	Media        []bus.MediaFile // media files from tool results (e.g. generated images)
	Deliverables []string        // actual content from tool outputs (e.g. written file text, image prompt)
}

// DelegateArtifacts holds forwarded artifacts from delegation results.
// Used to accumulate artifacts from intermediate completions until the final
// announce fires. New artifact types (files, voice, etc.) should be added here.
type DelegateArtifacts struct {
	Media            []bus.MediaFile         // media files to forward (images, documents, audio, etc.)
	Results          []DelegateResultSummary // result summaries from completed delegations
	CompletedTaskIDs []string                // team task IDs auto-completed by delegations (for announce context)
}

// DelegateResultSummary is a compact representation of a delegation result
// included in the final announce so the lead has all results in one message.
type DelegateResultSummary struct {
	AgentKey     string
	DisplayName  string   // target agent display name
	Content      string
	HasMedia     bool
	Deliverables []string // actual content from tool outputs
}

// AgentRunFunc runs an agent by key with the given request.
// This callback is injected from the cmd layer to avoid tools→agent import cycle.
type AgentRunFunc func(ctx context.Context, agentKey string, req DelegateRunRequest) (*DelegateRunResult, error)

// DelegateResult is the outcome of a delegation.
type DelegateResult struct {
	Content      string
	Iterations   int
	DelegationID string   // for async: the delegation ID to track/cancel
	TeamTaskID   string   // auto-created or provided team task ID (for tracing)
	Media        []bus.MediaFile // media files from delegation result
}

// linkSettings holds per-user restriction rules from agent_links.settings JSONB.
// NOTE: This is NOT the same as other_config.description (summoning prompt).
type linkSettings struct {
	RequireRole string   `json:"require_role"`
	UserAllow   []string `json:"user_allow"`
	UserDeny    []string `json:"user_deny"`
}

// MediaPathLoader resolves a media ID to a local file path.
// Used to propagate parent images to delegates without importing the media package.
type MediaPathLoader interface {
	LoadPath(id string) (string, error)
}

// DelegateManager manages inter-agent delegation lifecycle.
// Similar to SubagentManager but delegates to fully-configured named agents.
type DelegateManager struct {
	runAgent     AgentRunFunc
	linkStore    store.AgentLinkStore
	agentStore   store.AgentStore
	teamStore    store.TeamStore     // optional: enables auto-complete of team tasks
	sessionStore store.SessionStore  // optional: enables session cleanup
	mediaLoader  MediaPathLoader    // optional: enables image propagation to delegates
	msgBus       *bus.MessageBus     // for event broadcast + async announce (PublishInbound)
	hookEngine   *hooks.Engine       // optional: quality gate evaluation

	active            sync.Map // delegationID → *DelegationTask
	pendingArtifacts  sync.Map // sourceAgentID string → *DelegateArtifacts
	progressSent      sync.Map // "sourceAgentID:chatID" → true (dedup grouped notifications)
	progressEnabled   bool     // send "Your team is working on it..." to chat (default: false/off)
	completedMu       sync.Mutex
	completedSessions []string // session keys pending cleanup
}

// NewDelegateManager creates a new delegation manager.
func NewDelegateManager(
	runAgent AgentRunFunc,
	linkStore store.AgentLinkStore,
	agentStore store.AgentStore,
	msgBus *bus.MessageBus,
) *DelegateManager {
	return &DelegateManager{
		runAgent:   runAgent,
		linkStore:  linkStore,
		agentStore: agentStore,
		msgBus:     msgBus,
	}
}

// SetTeamStore enables auto-completion of team tasks on delegation success.
func (dm *DelegateManager) SetTeamStore(ts store.TeamStore) {
	dm.teamStore = ts
}

// SetSessionStore enables session cleanup after team tasks complete.
func (dm *DelegateManager) SetSessionStore(ss store.SessionStore) {
	dm.sessionStore = ss
}

// SetHookEngine enables quality gate evaluation on delegation results.
func (dm *DelegateManager) SetHookEngine(engine *hooks.Engine) {
	dm.hookEngine = engine
}

// SetMediaLoader enables image propagation from parent to delegate agents.
func (dm *DelegateManager) SetMediaLoader(ml MediaPathLoader) {
	dm.mediaLoader = ml
}

// SetProgressEnabled toggles "Your team is working on it..." chat notifications.
func (dm *DelegateManager) SetProgressEnabled(enabled bool) {
	dm.progressEnabled = enabled
}

// HandleActivityEvent updates the activity tracking for a delegation.
// Called from the bus event subscriber when an agent.activity event arrives.
// delegationID is extracted from the event's DelegationID field.
func (dm *DelegateManager) HandleActivityEvent(delegationID, phase, tool string) {
	if delegationID == "" {
		return
	}
	// O(1) lookup: active map is keyed by delegationID.
	val, ok := dm.active.Load(delegationID)
	if !ok {
		return
	}
	val.(*DelegationTask).UpdateActivity(phase, tool)
}

