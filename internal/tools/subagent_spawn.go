package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/childrun"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// SpawnReceipt identifies both the in-memory runtime task and its durable
// completion row. CompletionID is empty only when no task store is configured.
type SpawnReceipt struct {
	TaskID       string
	CompletionID uuid.UUID
	Message      string
}

// SpawnWithReceipt creates an asynchronous self-clone and returns identifiers
// that remain useful if the automatic parent announcement cannot be delivered.
func (sm *SubagentManager) SpawnWithReceipt(
	ctx context.Context,
	parentID string,
	depth int,
	task, label, modelOverride string,
	channel, chatID, peerKind string,
	callback AsyncCallback,
) (SpawnReceipt, error) {
	finishLifecycle, ok := sm.beginLifecycleOperation()
	if !ok {
		return SpawnReceipt{}, fmt.Errorf("subagent manager is closed")
	}
	lifecycleTransferred := false
	defer func() {
		if !lifecycleTransferred {
			finishLifecycle()
		}
	}()

	if err := validateDelegationChildRunMode(ctx, "spawn", "async"); err != nil {
		return SpawnReceipt{}, err
	}
	cfg := sm.effectiveSpawnConfig(ctx)
	depth = subagentDepthFromContext(ctx, depth)
	if depth >= cfg.MaxSpawnDepth {
		return SpawnReceipt{}, fmt.Errorf("spawn depth limit reached (%d/%d)", depth, cfg.MaxSpawnDepth)
	}

	scope := subagentScopeFromContext(ctx)
	if scope.RootAgentKey == "" {
		scope.RootAgentKey = parentID
	}
	if scope.TenantID == uuid.Nil || scope.RootAgentID == uuid.Nil || scope.RootAgentKey == "" {
		return SpawnReceipt{}, fmt.Errorf("spawn requires tenant and root-agent context")
	}
	parentTaskID := subagentTaskIDFromContext(ctx)
	admissionParentID := subagentAdmissionParentID(parentTaskID, scope)
	subTask := newSubagentTask(ctx, scope, parentTaskID, depth, task, label, modelOverride, channel, chatID, peerKind, cfg)
	admissionParentID, admissionDepth := childRunContinuationLineage(
		ctx,
		admissionParentID,
		subTask.Depth,
	)
	// Detach from parent's cancellation chain so subagent survives after parent run completes.
	// WithoutCancel preserves all context values (agent ID, workspace, trace info, etc.)
	// but parent Done() no longer propagates. Manual cancel via taskCancel() still works.
	detached := context.WithoutCancel(ctx)
	// The subagent runs after the parent's turn has ended, so the parent's
	// post-turn drain has already fired by the time it calls team_tasks. Give
	// this run its own tracker, drained when the subagent finishes, so tasks it
	// creates are dispatched and the team create lock it takes is released.
	detached, drainTeamDispatch := InjectTeamDispatch(detached, sm.postTurn)
	taskCtx, taskCancel := context.WithCancel(detached)
	subTask.cancelFunc = taskCancel

	if sm.taskStore != nil {
		subTask.dbID = store.GenNewID()
	}

	var ticket *orchestration.ChildRunTicket
	var err error
	var iterations int
	ticket, err = sm.admission.Enqueue(taskCtx, orchestration.ChildRunConstraints{
		TenantID:     scope.TenantID,
		RootAgentID:  scope.RootAgentID,
		RootLimit:    cfg.MaxConcurrent,
		TaskID:       subTask.ID,
		ParentTaskID: admissionParentID,
		ParentFanout: cfg.MaxChildrenPerAgent,
		Depth:        admissionDepth,
	}, func(runCtx context.Context, lease *orchestration.ChildRunLease) {
		defer drainTeamDispatch()
		sm.markTaskRunning(subTask)
		iterations = sm.executeTask(withSubagentExecution(runCtx, scope, subTask.ID, subTask.Depth, lease), subTask)
		lease.Release()
	})
	if err != nil {
		taskCancel()
		return SpawnReceipt{}, err
	}
	subTask.admissionTicket = ticket

	if _, err := sm.acceptTask(taskCtx, subTask); err != nil {
		ticket.Cancel()
		taskCancel()
		return SpawnReceipt{}, err
	}
	if err := ticket.Activate(); err != nil {
		sm.rollbackRejectedTask(taskCtx, subTask, err)
		taskCancel()
		return SpawnReceipt{}, err
	}
	announceCtx := context.WithoutCancel(taskCtx)
	completion := func() {
		defer finishLifecycle()
		<-ticket.Done()
		sm.finishTicketTask(announceCtx, subTask, ticket, iterations)
		terminalPersisted := sm.persistStatus(announceCtx, subTask, iterations) == nil
		taskCancel()
		sm.announceTask(announceCtx, subTask, callback, iterations, terminalPersisted)
	}
	lifecycleTransferred = true
	go completion()

	slog.Info("subagent queued", "id", subTask.ID, "parent_task", parentTaskID, "depth", subTask.Depth, "label", subTask.Label)

	message := fmt.Sprintf("Spawned subagent '%s' (id=%s, depth=%d) for task: %s",
		subTask.Label, subTask.ID, subTask.Depth, truncate(task, 100))
	return SpawnReceipt{
		TaskID:       subTask.ID,
		CompletionID: subTask.dbID,
		Message:      message,
	}, nil
}

// Spawn preserves the existing manager API for internal control flows.
func (sm *SubagentManager) Spawn(
	ctx context.Context,
	parentID string,
	depth int,
	task, label, modelOverride string,
	channel, chatID, peerKind string,
	callback AsyncCallback,
) (string, error) {
	receipt, err := sm.SpawnWithReceipt(
		ctx, parentID, depth, task, label, modelOverride,
		channel, chatID, peerKind, callback,
	)
	return receipt.Message, err
}

// RunSync executes a subagent task synchronously, blocking until completion.
func (sm *SubagentManager) RunSync(
	ctx context.Context,
	parentID string,
	depth int,
	task, label, modelOverride string,
	channel, chatID string,
) (string, []bus.MediaFile, int, error) {
	finishLifecycle, ok := sm.beginLifecycleOperation()
	if !ok {
		return "", nil, 0, fmt.Errorf("subagent manager is closed")
	}
	defer finishLifecycle()

	cfg := sm.effectiveSpawnConfig(ctx)
	depth = subagentDepthFromContext(ctx, depth)
	if depth >= cfg.MaxSpawnDepth {
		return "", nil, 0, fmt.Errorf("spawn depth limit reached (%d/%d)", depth, cfg.MaxSpawnDepth)
	}

	scope := subagentScopeFromContext(ctx)
	if scope.RootAgentKey == "" {
		scope.RootAgentKey = parentID
	}
	if scope.TenantID == uuid.Nil || scope.RootAgentID == uuid.Nil || scope.RootAgentKey == "" {
		return "", nil, 0, fmt.Errorf("spawn requires tenant and root-agent context")
	}
	parentTaskID := subagentTaskIDFromContext(ctx)
	admissionParentID := subagentAdmissionParentID(parentTaskID, scope)
	subTask := newSubagentTask(ctx, scope, parentTaskID, depth, task, label, modelOverride, channel, chatID, "", cfg)
	admissionParentID, admissionDepth := childRunContinuationLineage(
		ctx,
		admissionParentID,
		subTask.Depth,
	)
	if sm.taskStore != nil {
		subTask.dbID = store.GenNewID()
	}

	taskCtx, taskCancel := context.WithCancel(ctx)
	subTask.cancelFunc = taskCancel
	constraints := orchestration.ChildRunConstraints{
		TenantID:     scope.TenantID,
		RootAgentID:  scope.RootAgentID,
		RootLimit:    cfg.MaxConcurrent,
		TaskID:       subTask.ID,
		ParentTaskID: admissionParentID,
		ParentFanout: cfg.MaxChildrenPerAgent,
		Depth:        admissionDepth,
	}
	var iterations int
	var acceptErr error
	acceptedBeforeActivate := false
	run := func(runCtx context.Context, lease *orchestration.ChildRunLease) {
		if !acceptedBeforeActivate {
			if _, acceptErr = sm.acceptTask(runCtx, subTask); acceptErr != nil {
				lease.Release()
				return
			}
		}
		sm.markTaskRunning(subTask)
		iterations = sm.executeTask(withSubagentExecution(runCtx, scope, subTask.ID, subTask.Depth, lease), subTask)
		lease.Release()
		sm.persistStatus(runCtx, subTask, iterations)
	}
	slog.Info("subagent sync queued", "id", subTask.ID, "parent_task", parentTaskID, "depth", subTask.Depth, "label", subTask.Label)
	if parentLease := childRunLeaseFromContext(ctx); parentLease != nil {
		sm.setTaskWaiting(parentTaskID, true)
		err := parentLease.Continue(taskCtx, constraints, run)
		sm.setTaskWaiting(parentTaskID, false)
		if err != nil {
			if sm.taskAccepted(subTask.ID) && !isTerminalTaskStatus(subTask.Status) {
				sm.markTaskFailed(subTask, err)
				sm.persistStatus(taskCtx, subTask, iterations)
			}
			taskCancel()
			return "", nil, iterations, err
		}
		if acceptErr != nil {
			taskCancel()
			return "", nil, iterations, acceptErr
		}
	} else {
		ticket, err := sm.admission.Enqueue(taskCtx, constraints, run)
		if err != nil {
			taskCancel()
			return "", nil, 0, err
		}
		subTask.admissionTicket = ticket
		if _, err := sm.acceptTask(taskCtx, subTask); err != nil {
			ticket.Cancel()
			taskCancel()
			return "", nil, 0, err
		}
		acceptedBeforeActivate = true
		if err := ticket.Activate(); err != nil {
			sm.rollbackRejectedTask(taskCtx, subTask, err)
			taskCancel()
			return "", nil, 0, err
		}
		<-ticket.Done()
		sm.finishTicketTask(taskCtx, subTask, ticket, iterations)
	}
	taskCancel()

	if subTask.Status == TaskStatusFailed {
		return subTask.Result, nil, iterations, fmt.Errorf("subagent failed: %s", subTask.Result)
	}

	media := append([]bus.MediaFile(nil), subTask.Media...)
	return subTask.Result, media, iterations, nil
}

// effectiveSpawnConfig applies per-agent overrides and edition ceilings once so
// async admission, sync admission, and pipeline batching use the same limits.
func (sm *SubagentManager) effectiveSpawnConfig(ctx context.Context) SubagentConfig {
	cfg := sm.effectiveConfig(ctx)
	ed := edition.Current()
	if ed.MaxSubagentDepth > 0 && cfg.MaxSpawnDepth > ed.MaxSubagentDepth {
		cfg.MaxSpawnDepth = ed.MaxSubagentDepth
	}
	return cfg
}

func newSubagentTask(
	ctx context.Context,
	scope TaskScope,
	parentTaskID string,
	depth int,
	task, label, modelOverride string,
	channel, chatID, peerKind string,
	cfg SubagentConfig,
) *SubagentTask {
	if label == "" {
		label = truncate(task, 50)
	}
	mediaPathPrefix := ""
	if IsDelegationArtifactRun(ctx) {
		mediaPathPrefix = "outputs"
	}
	return &SubagentTask{
		ID:                  generateSubagentID(),
		ParentID:            scope.RootAgentKey,
		ParentTaskID:        parentTaskID,
		Task:                task,
		Label:               label,
		Status:              TaskStatusQueued,
		Depth:               depth + 1,
		Model:               modelOverride,
		OriginChannel:       channel,
		OriginChatID:        chatID,
		OriginPeerKind:      peerKind,
		OriginLocalKey:      ToolLocalKeyFromCtx(ctx),
		OriginUserID:        store.UserIDFromContext(ctx),
		OriginSenderID:      store.SenderIDFromContext(ctx),
		OriginRole:          store.RoleFromContext(ctx),
		OriginSessionKey:    ToolSessionKeyFromCtx(ctx),
		OriginAgentID:       scope.RootAgentID,
		OriginTenantID:      scope.TenantID,
		RootAgentID:         scope.RootAgentID,
		RootAgentKey:        scope.RootAgentKey,
		OriginTraceID:       tracing.TraceIDFromContext(ctx),
		OriginRootSpanID:    tracing.ParentSpanIDFromContext(ctx),
		OriginContextWindow: store.AgentContextWindowFromContext(ctx),
		OriginMaxTokens:     store.AgentMaxTokensFromContext(ctx),
		Workspace:           ToolWorkspaceFromCtx(ctx),
		MediaPathPrefix:     mediaPathPrefix,
		CreatedAt:           time.Now().UnixMilli(),
		spawnConfig:         cfg,
	}
}

func (sm *SubagentManager) markTaskRunning(task *SubagentTask) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if task.Status == TaskStatusCancelled {
		return
	}
	task.Status = TaskStatusRunning
}

func (sm *SubagentManager) setTaskWaiting(taskID string, waiting bool) {
	if taskID == "" {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	task := sm.tasks[taskID]
	if task == nil || isTerminalTaskStatus(task.Status) {
		return
	}
	if waiting {
		task.Status = TaskStatusWaiting
	} else {
		task.Status = TaskStatusRunning
	}
}

func (sm *SubagentManager) markTaskFailed(task *SubagentTask, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if !isTerminalTaskStatus(task.Status) {
		task.Status = TaskStatusFailed
		task.Result = err.Error()
		task.CompletedAt = time.Now().UnixMilli()
	}
}

func (sm *SubagentManager) acceptTask(ctx context.Context, task *SubagentTask) (bool, error) {
	sm.mu.Lock()
	if _, exists := sm.tasks[task.ID]; exists {
		sm.mu.Unlock()
		return false, fmt.Errorf("subagent task ID collision: %s", task.ID)
	}
	sm.tasks[task.ID] = task
	sm.mu.Unlock()
	if err := sm.persistCreate(ctx, task); err != nil {
		sm.mu.Lock()
		if sm.tasks[task.ID] == task {
			delete(sm.tasks, task.ID)
		}
		sm.mu.Unlock()
		return false, err
	}
	if task.spawnConfig.ArchiveAfterMinutes > 0 {
		sm.startArchiveSweeper()
	}
	return true, nil
}

func (sm *SubagentManager) taskAccepted(taskID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, exists := sm.tasks[taskID]
	return exists
}

func (sm *SubagentManager) rollbackRejectedTask(ctx context.Context, task *SubagentTask, err error) {
	sm.markTaskFailed(task, err)
	sm.persistStatus(ctx, task, 0)
	sm.mu.Lock()
	if sm.tasks[task.ID] == task {
		delete(sm.tasks, task.ID)
	}
	sm.mu.Unlock()
}

func subagentAdmissionParentID(parentTaskID string, scope TaskScope) string {
	if parentTaskID != "" {
		return parentTaskID
	}
	return "root:" + scope.RootAgentID.String()
}

func (sm *SubagentManager) finishTicketTask(
	ctx context.Context,
	task *SubagentTask,
	ticket *orchestration.ChildRunTicket,
	iterations int,
) {
	sm.mu.RLock()
	terminal := isTerminalTaskStatus(task.Status)
	sm.mu.RUnlock()
	if terminal {
		return
	}
	err := ticket.Err()
	if err == nil {
		err = fmt.Errorf("child run ended without terminal task state")
	}
	sm.markTaskFailed(task, err)
	sm.persistStatus(ctx, task, iterations)
}
