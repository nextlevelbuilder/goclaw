package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

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

	// Apply edition ceilings (Lite edition enforces lower limits).
	ed := edition.Current()
	if ed.MaxSubagentConcurrent > 0 && cfg.MaxConcurrent > ed.MaxSubagentConcurrent {
		cfg.MaxConcurrent = ed.MaxSubagentConcurrent
	}
	if ed.MaxSubagentDepth > 0 && cfg.MaxSpawnDepth > ed.MaxSubagentDepth {
		cfg.MaxSpawnDepth = ed.MaxSubagentDepth
	}

	sm.mu.Lock()

	// Check depth limit
	if depth >= cfg.MaxSpawnDepth {
		sm.mu.Unlock()
		return "", fmt.Errorf("spawn depth limit reached (%d/%d)", depth, cfg.MaxSpawnDepth)
	}

	// Check concurrent limit (scoped per tenant for isolation).
	tenantID := store.TenantIDFromContext(ctx)
	running := 0
	for _, t := range sm.tasks {
		if t.Status == TaskStatusRunning && t.OriginTenantID == tenantID {
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
		OriginSenderID:    store.SenderIDFromContext(ctx),
		OriginRole:        store.RoleFromContext(ctx),
		OriginSessionKey:  ToolSessionKeyFromCtx(ctx),
		OriginTenantID:    store.TenantIDFromContext(ctx),
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

	// Assign DB UUID inside lock to avoid race with runTask goroutine.
	if sm.taskStore != nil {
		subTask.dbID = store.GenNewID()
	}

	sm.tasks[id] = subTask
	sm.mu.Unlock()

	slog.Info("subagent spawned", "id", id, "parent", parentID, "depth", subTask.Depth, "label", label)

	// Persist to DB (fire-and-forget).
	if sm.taskStore != nil {
		go sm.persistCreate(taskCtx, subTask)
	}

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

	// Apply edition ceilings (same as Spawn).
	ed := edition.Current()
	if ed.MaxSubagentConcurrent > 0 && cfg.MaxConcurrent > ed.MaxSubagentConcurrent {
		cfg.MaxConcurrent = ed.MaxSubagentConcurrent
	}
	if ed.MaxSubagentDepth > 0 && cfg.MaxSpawnDepth > ed.MaxSubagentDepth {
		cfg.MaxSpawnDepth = ed.MaxSubagentDepth
	}

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
		OriginSenderID:   store.SenderIDFromContext(ctx),
		OriginRole:       store.RoleFromContext(ctx),
		OriginSessionKey: ToolSessionKeyFromCtx(ctx),
		OriginTenantID:   store.TenantIDFromContext(ctx),
		OriginTraceID:    tracing.TraceIDFromContext(ctx),
		OriginRootSpanID: tracing.ParentSpanIDFromContext(ctx),
		CreatedAt:        time.Now().UnixMilli(),
		spawnConfig:      cfg,
	}
	if sm.taskStore != nil {
		subTask.dbID = store.GenNewID()
	}
	sm.tasks[id] = subTask
	sm.mu.Unlock()

	slog.Info("subagent sync started", "id", id, "parent", parentID, "depth", subTask.Depth, "label", label)

	if sm.taskStore != nil {
		sm.persistCreate(ctx, subTask)
	}

	iterations := sm.executeTask(ctx, subTask)

	if subTask.Status == TaskStatusFailed {
		return subTask.Result, iterations, fmt.Errorf("subagent failed: %s", subTask.Result)
	}

	return subTask.Result, iterations, nil
}
