// Package tools provides the subagent system for spawning child agent instances.
//
// Subagents run in background goroutines with restricted tool access.
// Key GoClaw constraints:
//   - Depth limit: configurable maxSpawnDepth (default 1)
//   - Max children per parent: configurable (default 5)
//   - Max executing descendants per root agent: configurable (default 20)
//   - Auto-archive after configurable TTL (default 60 min)
//   - Tool deny lists: ALWAYS_DENY + LEAF_DENY at max depth
//   - Results announced back to parent via message bus
package tools

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/childrun"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

var ErrSubagentLifecycleDrainTimeout = errors.New("subagent_lifecycle_drain_timeout")

// SubagentConfig configures the subagent system.
type SubagentConfig struct {
	MaxConcurrent       int    // executing descendants per root agent (default 20)
	MaxSpawnDepth       int    // max nesting depth (default 1)
	MaxChildrenPerAgent int    // max children per parent (default 5)
	ArchiveAfterMinutes int    // auto-archive completed tasks (default 60)
	MaxRetries          int    // max LLM call retries on error (default 2)
	Model               string // model override for subagents (empty = inherit)
}

// Subagent task status constants.
const (
	TaskStatusQueued    = "queued"
	TaskStatusRunning   = "running"
	TaskStatusWaiting   = string(orchestration.ChildRunWaitingChild)
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// SubagentTask tracks a running or completed subagent.
type SubagentTask struct {
	ID                string          `json:"id"`
	ParentID          string          `json:"parentId"`
	Task              string          `json:"task"`
	Label             string          `json:"label"`
	Status            string          `json:"status"` // "running", "completed", "failed", "cancelled"
	Result            string          `json:"result,omitempty"`
	Depth             int             `json:"depth"`
	Model             string          `json:"model,omitempty"` // model override for this subagent
	TotalInputTokens  int64           `json:"totalInputTokens,omitempty"`
	TotalOutputTokens int64           `json:"totalOutputTokens,omitempty"`
	OriginChannel     string          `json:"originChannel,omitempty"`
	OriginChatID      string          `json:"originChatId,omitempty"`
	OriginPeerKind    string          `json:"originPeerKind,omitempty"`   // "direct" or "group" (for session key building)
	OriginLocalKey    string          `json:"originLocalKey,omitempty"`   // composite key with topic/thread suffix for routing
	OriginUserID      string          `json:"originUserId,omitempty"`     // parent's userID for per-user scoping propagation
	OriginSenderID    string          `json:"originSenderId,omitempty"`   // real acting sender; preserves permission attribution in announce re-ingress (#915)
	OriginRole        string          `json:"originRole,omitempty"`       // parent's RBAC role; bypasses per-user grants for admin/operator/owner in re-ingress (#915)
	OriginSessionKey  string          `json:"originSessionKey,omitempty"` // exact parent session key for announce routing (WS uses non-standard format)
	CreatedAt         int64           `json:"createdAt"`
	CompletedAt       int64           `json:"completedAt,omitempty"`
	Media             []bus.MediaFile `json:"-"` // media files from tool results
	Workspace         string          `json:"-"` // physical root used only to derive safe completion media paths
	MediaPathPrefix   string          `json:"-"` // logical prefix, e.g. outputs/ in a delegation exchange
	OriginAgentID     uuid.UUID       `json:"-"` // parent agent UUID for usage caps and scoped tools
	OriginTenantID    uuid.UUID       `json:"-"` // parent's tenant for announce routing
	RootAgentID       uuid.UUID       `json:"-"`
	RootAgentKey      string          `json:"-"`
	ParentTaskID      string          `json:"parentTaskId,omitempty"`
	OriginTraceID     uuid.UUID       `json:"-"` // parent trace for announce linking
	OriginRootSpanID  uuid.UUID       `json:"-"` // parent agent's root span ID
	// Capture caller-specific budgets because one process manager serves many agents.
	OriginContextWindow int                `json:"-"`
	OriginMaxTokens     int                `json:"-"`
	cancelFunc          context.CancelFunc `json:"-"` // per-task context cancel
	spawnConfig         SubagentConfig     `json:"-"` // resolved config at spawn time (per-agent override merged)
	dbID                uuid.UUID          `json:"-"` // persistent DB UUID (zero if not persisted)
	admissionTicket     *orchestration.ChildRunTicket
}

// TaskScope is the authorization boundary for every in-memory task action.
// A task ID alone never authorizes lookup, cancellation, steering, or cleanup.
type TaskScope struct {
	TenantID     uuid.UUID
	RootAgentID  uuid.UUID
	RootAgentKey string
}

// SubagentManager manages the lifecycle of spawned subagents.
type SubagentManager struct {
	mu          sync.RWMutex
	tasks       map[string]*SubagentTask
	config      SubagentConfig
	provider    providers.Provider  // default provider (fallback)
	providerReg *providers.Registry // registry for resolving parent's provider
	model       string
	msgBus      *bus.MessageBus

	// createTools builds a tool registry for subagents (without spawn/subagent tools).
	createTools     func() *Registry
	announceQueue   *AnnounceQueue          // optional: batches announces with debounce
	taskStore       store.SubagentTaskStore // optional: durable async completion ledger
	usageCaps       *usagecaps.Service
	admission       *orchestration.ChildRunAdmission
	sweeperOnce     sync.Once
	sweeperStop     chan struct{}
	sweeperDone     chan struct{}
	sweeperStarted  bool
	lifecycleMu     sync.Mutex
	lifecycleClosed bool
	lifecycleWG     sync.WaitGroup
	closeOnce       sync.Once
	closeDone       chan struct{}
	now             func() time.Time
	// Defaults used only when a task was created without caller-specific context.
	contextWindow int
	maxTokens     int
}

// NewSubagentManager creates a new subagent manager.
func NewSubagentManager(
	provider providers.Provider,
	providerReg *providers.Registry,
	model string,
	msgBus *bus.MessageBus,
	createTools func() *Registry,
	cfg SubagentConfig,
) *SubagentManager {
	return NewSubagentManagerWithAdmission(
		provider,
		providerReg,
		model,
		msgBus,
		createTools,
		cfg,
		orchestration.NewChildRunAdmission(32, 128),
	)
}

// NewSubagentManagerWithAdmission creates a manager sharing the process-owned
// child-run admission controller with other child execution paths.
func NewSubagentManagerWithAdmission(
	provider providers.Provider,
	providerReg *providers.Registry,
	model string,
	msgBus *bus.MessageBus,
	createTools func() *Registry,
	cfg SubagentConfig,
	admission *orchestration.ChildRunAdmission,
) *SubagentManager {
	if admission == nil {
		admission = orchestration.NewChildRunAdmission(32, 128)
	}
	return &SubagentManager{
		tasks:       make(map[string]*SubagentTask),
		config:      cfg,
		provider:    provider,
		providerReg: providerReg,
		model:       model,
		msgBus:      msgBus,
		createTools: createTools,
		admission:   admission,
		sweeperStop: make(chan struct{}),
		sweeperDone: make(chan struct{}),
		closeDone:   make(chan struct{}),
		now:         time.Now,
	}
}

// SetAnnounceQueue sets the announce queue for batched announce delivery.
// If set, runTask() enqueues announces instead of publishing directly.
func (sm *SubagentManager) SetAnnounceQueue(q *AnnounceQueue) {
	sm.announceQueue = q
}

// SetTaskStore sets the persistent store for task lifecycle and async retrieval.
func (sm *SubagentManager) SetTaskStore(s store.SubagentTaskStore) {
	sm.taskStore = s
}

func (sm *SubagentManager) SetUsageCapService(s *usagecaps.Service) {
	sm.usageCaps = s
}

// SetAgentBudget records the manager's fallback agent budget. Spawned tasks
// normally use their caller-specific values captured from context.
func (sm *SubagentManager) SetAgentBudget(window, maxTokens int) {
	sm.contextWindow = window
	sm.maxTokens = maxTokens
}

// effectiveConfig returns the per-agent context override merged with defaults,
// or falls back to sm.config when no override is present.
func (sm *SubagentManager) effectiveConfig(ctx context.Context) SubagentConfig {
	override := SubagentConfigFromCtx(ctx)
	if override == nil {
		return sm.config
	}
	cfg := sm.config
	if override.MaxConcurrent > 0 {
		cfg.MaxConcurrent = override.MaxConcurrent
	}
	if override.MaxSpawnDepth > 0 {
		cfg.MaxSpawnDepth = override.MaxSpawnDepth
	}
	if override.MaxChildrenPerAgent > 0 {
		cfg.MaxChildrenPerAgent = override.MaxChildrenPerAgent
	}
	if override.ArchiveAfterMinutes > 0 {
		cfg.ArchiveAfterMinutes = override.ArchiveAfterMinutes
	}
	if override.MaxRetries > 0 {
		cfg.MaxRetries = override.MaxRetries
	}
	if override.Model != "" {
		cfg.Model = override.Model
	}
	return cfg
}

// SubagentDenyAlways is the list of tools always denied to subagents.
var SubagentDenyAlways = []string{
	"gateway",
	"agents_list",
	"whatsapp_login",
	"session_status",
	"cron",
	"memory_search",
	"memory_get",
	"sessions_send",
	"team_tasks", // subagents must not use team orchestration
}

// SubagentDenyLeaf is the additional deny list for subagents at max depth.
var SubagentDenyLeaf = []string{
	"sessions_list",
	"sessions_history",
	"sessions_spawn",
	"spawn",
}
