// Package tools provides the subagent system for spawning child agent instances.
//
// Subagents run in background goroutines with restricted tool access.
// Key constraints from OpenClaw spec:
//   - Depth limit: configurable maxSpawnDepth (default 3)
//   - Max children per parent: configurable (default 8)
//   - Auto-archive after configurable TTL (default 30 min)
//   - Tool deny lists: ALWAYS_DENY + LEAF_DENY at max depth
//   - Results announced back to parent via message bus
package tools

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SubagentConfig configures the subagent system.
type SubagentConfig struct {
	MaxConcurrent       int    // max concurrent subagents (default 4)
	MaxSpawnDepth       int    // max nesting depth (default 3)
	MaxChildrenPerAgent int    // max children per parent (default 8)
	ArchiveAfterMinutes int    // auto-archive completed tasks (default 30)
	MaxRetries          int    // max LLM call retries on error (default 2)
	Model               string // model override for subagents (empty = inherit)
}

// Subagent task status constants.
const (
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// SubagentTask tracks a running or completed subagent.
type SubagentTask struct {
	ID              string `json:"id"`
	ParentID        string `json:"parentId"`
	Task            string `json:"task"`
	Label           string `json:"label"`
	Status          string `json:"status"` // "running", "completed", "failed", "cancelled"
	Result          string `json:"result,omitempty"`
	Depth           int    `json:"depth"`
	Model           string `json:"model,omitempty"`           // model override for this subagent
	TotalInputTokens  int64 `json:"totalInputTokens,omitempty"`
	TotalOutputTokens int64 `json:"totalOutputTokens,omitempty"`
	OriginChannel    string `json:"originChannel,omitempty"`
	OriginChatID     string `json:"originChatId,omitempty"`
	OriginPeerKind   string `json:"originPeerKind,omitempty"`  // "direct" or "group" (for session key building)
	OriginLocalKey   string `json:"originLocalKey,omitempty"`  // composite key with topic/thread suffix for routing
	OriginUserID     string `json:"originUserId,omitempty"`    // parent's userID for per-user scoping propagation
	OriginSenderID   string `json:"originSenderId,omitempty"`  // real acting sender; preserves permission attribution in announce re-ingress (#915)
	OriginRole       string `json:"originRole,omitempty"`      // parent's RBAC role; bypasses per-user grants for admin/operator/owner in re-ingress (#915)
	OriginSessionKey string `json:"originSessionKey,omitempty"` // exact parent session key for announce routing (WS uses non-standard format)
	CreatedAt        int64  `json:"createdAt"`
	CompletedAt      int64  `json:"completedAt,omitempty"`
	Media            []bus.MediaFile `json:"-"` // media files from tool results
	OriginTenantID   uuid.UUID `json:"-"` // parent's tenant for announce routing
	OriginTraceID    uuid.UUID `json:"-"` // parent trace for announce linking
	OriginRootSpanID uuid.UUID `json:"-"` // parent agent's root span ID
	cancelFunc       context.CancelFunc `json:"-"` // per-task context cancel
	spawnConfig      SubagentConfig `json:"-"` // resolved config at spawn time (per-agent override merged)
	dbID             uuid.UUID `json:"-"` // persistent DB UUID (zero if not persisted)
}

// SubagentManager manages the lifecycle of spawned subagents.
type SubagentManager struct {
	mu          sync.RWMutex
	tasks       map[string]*SubagentTask
	config      SubagentConfig
	provider    providers.Provider   // default provider (fallback)
	providerReg *providers.Registry  // registry for resolving parent's provider
	model       string
	msgBus      *bus.MessageBus

	// createTools builds a tool registry for subagents (without spawn/subagent tools).
	createTools   func() *Registry
	announceQueue *AnnounceQueue          // optional: batches announces with debounce
	taskStore     store.SubagentTaskStore // optional: persists tasks to DB (fire-and-forget)
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
	return &SubagentManager{
		tasks:       make(map[string]*SubagentTask),
		config:      cfg,
		provider:    provider,
		providerReg: providerReg,
		model:       model,
		msgBus:      msgBus,
		createTools: createTools,
	}
}

// SetAnnounceQueue sets the announce queue for batched announce delivery.
// If set, runTask() enqueues announces instead of publishing directly.
func (sm *SubagentManager) SetAnnounceQueue(q *AnnounceQueue) {
	sm.announceQueue = q
}

// SetTaskStore sets the persistent store for subagent tasks (write-through, fire-and-forget).
func (sm *SubagentManager) SetTaskStore(s store.SubagentTaskStore) {
	sm.taskStore = s
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
