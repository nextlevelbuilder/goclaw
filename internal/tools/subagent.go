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
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// SubagentConfig configures the subagent system.
type SubagentConfig struct {
	MaxConcurrent       int    // max concurrent subagents (default 4)
	MaxSpawnDepth       int    // max nesting depth (default 3)
	MaxChildrenPerAgent int    // max children per parent (default 8)
	ArchiveAfterMinutes int    // auto-archive completed tasks (default 30)
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
	OriginChannel    string `json:"originChannel,omitempty"`
	OriginChatID     string `json:"originChatId,omitempty"`
	OriginPeerKind   string `json:"originPeerKind,omitempty"`  // "direct" or "group" (for session key building)
	OriginLocalKey   string `json:"originLocalKey,omitempty"`  // composite key with topic/thread suffix for routing
	OriginUserID     string `json:"originUserId,omitempty"`    // parent's userID for per-user scoping propagation
	OriginSessionKey string `json:"originSessionKey,omitempty"` // exact parent session key for announce routing (WS uses non-standard format)
	CreatedAt        int64  `json:"createdAt"`
	CompletedAt      int64  `json:"completedAt,omitempty"`
	Media            []bus.MediaFile `json:"-"` // media files from tool results
	OriginTraceID    uuid.UUID `json:"-"` // parent trace for announce linking
	OriginRootSpanID uuid.UUID `json:"-"` // parent agent's root span ID
	cancelFunc       context.CancelFunc `json:"-"` // per-task context cancel
	spawnConfig      SubagentConfig `json:"-"` // resolved config at spawn time (per-agent override merged)
}

// SubagentManager manages the lifecycle of spawned subagents.
type SubagentManager struct {
	mu       sync.RWMutex
	tasks    map[string]*SubagentTask
	config   SubagentConfig
	provider providers.Provider
	model    string
	msgBus   *bus.MessageBus

	// createTools builds a tool registry for subagents (without spawn/subagent tools).
	createTools   func() *Registry
	announceQueue *AnnounceQueue // optional: batches announces with debounce
}

// NewSubagentManager creates a new subagent manager.
func NewSubagentManager(
	provider providers.Provider,
	model string,
	msgBus *bus.MessageBus,
	createTools func() *Registry,
	cfg SubagentConfig,
) *SubagentManager {
	return &SubagentManager{
		tasks:       make(map[string]*SubagentTask),
		config:      cfg,
		provider:    provider,
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
	if override.Model != "" {
		cfg.Model = override.Model
	}
	return cfg
}

// CountRunningForParent returns the number of running tasks for a parent.
func (sm *SubagentManager) CountRunningForParent(parentID string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	count := 0
	for _, t := range sm.tasks {
		if t.ParentID == parentID && t.Status == TaskStatusRunning {
			count++
		}
	}
	return count
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
}

// SubagentDenyLeaf is the additional deny list for subagents at max depth.
var SubagentDenyLeaf = []string{
	"sessions_list",
	"sessions_history",
	"sessions_spawn",
	"spawn",
}

// Spawn creates a new subagent task that runs asynchronously.
// Returns immediately with a status message. The subagent runs in a goroutine.
// modelOverride optionally overrides the LLM model for this subagent (matching TS sessions-spawn-tool.ts).
func (sm *SubagentManager) Spawn(
	ctx context.Context,
	parentID string,
	depth int,
	task, label, modelOverride string,
	channel, chatID, peerKind string,
	callback AsyncCallback,
) (string, error) {
	cfg := sm.effectiveConfig(ctx)
	sm.mu.Lock()

	// Check depth limit
	if depth >= cfg.MaxSpawnDepth {
		sm.mu.Unlock()
		return "", fmt.Errorf("spawn depth limit reached (%d/%d)", depth, cfg.MaxSpawnDepth)
	}

	// Check concurrent limit
	running := 0
	for _, t := range sm.tasks {
		if t.Status == TaskStatusRunning {
			running++
		}
	}
	if running >= cfg.MaxConcurrent {
		sm.mu.Unlock()
		return "", fmt.Errorf("max concurrent subagents reached (%d/%d)", running, cfg.MaxConcurrent)
	}

	// Check per-parent children limit
	childCount := 0
	for _, t := range sm.tasks {
		if t.ParentID == parentID {
			childCount++
		}
	}
	if childCount >= cfg.MaxChildrenPerAgent {
		sm.mu.Unlock()
		return "", fmt.Errorf("max children per agent reached (%d/%d)", childCount, cfg.MaxChildrenPerAgent)
	}

	id := generateSubagentID()
	if label == "" {
		label = truncate(task, 50)
	}

	subTask := &SubagentTask{
		ID:               id,
		ParentID:         parentID,
		Task:             task,
		Label:            label,
		Status:           "running",
		Depth:            depth + 1,
		Model:            modelOverride,
		OriginChannel:    channel,
		OriginChatID:     chatID,
		OriginPeerKind:   peerKind,
		OriginLocalKey:    ToolLocalKeyFromCtx(ctx),
		OriginUserID:      store.UserIDFromContext(ctx),
		OriginSessionKey:  ToolSessionKeyFromCtx(ctx),
		OriginTraceID:     tracing.TraceIDFromContext(ctx),
		OriginRootSpanID:  tracing.ParentSpanIDFromContext(ctx),
		CreatedAt:         time.Now().UnixMilli(),
		spawnConfig:       cfg,
	}
	// Detach from parent's cancellation chain so subagent survives after parent run completes.
	// WithoutCancel preserves all context values (agent ID, workspace, trace info, etc.)
	// but parent Done() no longer propagates. Manual cancel via taskCancel() still works.
	detached := context.WithoutCancel(ctx)
	taskCtx, taskCancel := context.WithCancel(detached)
	subTask.cancelFunc = taskCancel

	sm.tasks[id] = subTask
	sm.mu.Unlock()

	slog.Info("subagent spawned", "id", id, "parent", parentID, "depth", subTask.Depth, "label", label)

	go sm.runTask(taskCtx, subTask, callback)

	return fmt.Sprintf("Spawned subagent '%s' (id=%s, depth=%d) for task: %s",
		label, id, subTask.Depth, truncate(task, 100)), nil
}

// RunSync executes a subagent task synchronously, blocking until completion.
func (sm *SubagentManager) RunSync(
	ctx context.Context,
	parentID string,
	depth int,
	task, label string,
	channel, chatID string,
) (string, int, error) {
	cfg := sm.effectiveConfig(ctx)

	sm.mu.Lock()

	if depth >= cfg.MaxSpawnDepth {
		sm.mu.Unlock()
		return "", 0, fmt.Errorf("spawn depth limit reached (%d/%d)", depth, cfg.MaxSpawnDepth)
	}

	id := generateSubagentID()
	if label == "" {
		label = truncate(task, 50)
	}

	subTask := &SubagentTask{
		ID:               id,
		ParentID:         parentID,
		Task:             task,
		Label:            label,
		Status:           "running",
		Depth:            depth + 1,
		OriginChannel:    channel,
		OriginChatID:     chatID,
		OriginLocalKey:   ToolLocalKeyFromCtx(ctx),
		OriginUserID:     store.UserIDFromContext(ctx),
		OriginTraceID:    tracing.TraceIDFromContext(ctx),
		OriginRootSpanID: tracing.ParentSpanIDFromContext(ctx),
		CreatedAt:        time.Now().UnixMilli(),
		spawnConfig:      cfg,
	}
	sm.tasks[id] = subTask
	sm.mu.Unlock()

	slog.Info("subagent sync started", "id", id, "parent", parentID, "depth", subTask.Depth, "label", label)

	iterations := sm.executeTask(ctx, subTask)

	if subTask.Status == TaskStatusFailed {
		return subTask.Result, iterations, fmt.Errorf("subagent failed: %s", subTask.Result)
	}

	return subTask.Result, iterations, nil
}
