package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	asyncCompletionKindKey         = "completion_kind"
	asyncCompletionKindSubagent    = "subagent"
	asyncCompletionKindDelegate    = "delegate"
	asyncCompletionRuntimeIDKey    = "runtime_task_id"
	asyncCompletionDeliveryKey     = "announcement_status"
	asyncCompletionDeliveryPending = "pending"
	asyncCompletionDeliveryDone    = "delivered"
	asyncCompletionDeliveryMissed  = "undelivered"
)

// detachedCtx creates a context that won't be cancelled but preserves tenant ID.
// Used for fire-and-forget DB writes that must succeed even after the parent ctx is cancelled.
func detachedCtx(ctx context.Context) context.Context {
	bg := context.Background()
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		bg = store.WithTenantID(bg, tid)
	}
	return bg
}

// persistCreate writes the accepted queued task before its admission ticket is
// activated. When a store is configured, execution is not accepted unless this
// write succeeds so every returned completion ID remains retrievable.
func (sm *SubagentManager) persistCreate(ctx context.Context, task *SubagentTask) error {
	if sm.taskStore == nil {
		return nil
	}

	dbCtx := detachedCtx(ctx)

	var sessionKey *string
	if task.OriginSessionKey != "" {
		s := task.OriginSessionKey
		sessionKey = &s
	}
	var model, provider, originChannel, originChatID, originPeerKind, originUserID *string
	if task.Model != "" {
		model = &task.Model
	}
	if p := ParentProviderFromCtx(ctx); p != "" {
		provider = &p
	}
	if task.OriginChannel != "" {
		originChannel = &task.OriginChannel
	}
	if task.OriginChatID != "" {
		originChatID = &task.OriginChatID
	}
	if task.OriginPeerKind != "" {
		originPeerKind = &task.OriginPeerKind
	}
	if task.OriginUserID != "" {
		originUserID = &task.OriginUserID
	}

	var spawnedBy *uuid.UUID
	if task.ParentTaskID != "" {
		sm.mu.RLock()
		if parent := sm.tasks[task.ParentTaskID]; parent != nil && parent.dbID != uuid.Nil &&
			taskMatchesScope(parent, TaskScope{
				TenantID: task.OriginTenantID, RootAgentID: task.RootAgentID, RootAgentKey: task.RootAgentKey,
			}) {
			parentID := parent.dbID
			spawnedBy = &parentID
		}
		sm.mu.RUnlock()
	}

	data := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: task.dbID},
		TenantID:       task.OriginTenantID,
		RootAgentID:    task.RootAgentID,
		ParentAgentKey: task.RootAgentKey,
		SessionKey:     sessionKey,
		Subject:        task.Label,
		Description:    task.Task,
		Status:         task.Status,
		Depth:          task.Depth,
		Model:          model,
		Provider:       provider,
		OriginChannel:  originChannel,
		OriginChatID:   originChatID,
		OriginPeerKind: originPeerKind,
		OriginUserID:   originUserID,
		SpawnedBy:      spawnedBy,
		Metadata: map[string]any{
			"root_agent_id":             task.RootAgentID.String(),
			"parent_task_id":            task.ParentTaskID,
			"depth":                     task.Depth,
			asyncCompletionKindKey:      asyncCompletionKindSubagent,
			asyncCompletionRuntimeIDKey: task.ID,
			asyncCompletionDeliveryKey:  asyncCompletionDeliveryPending,
		},
	}

	if err := retryAsyncPersistence(dbCtx, func(attemptCtx context.Context) error {
		return sm.taskStore.Create(attemptCtx, data)
	}); err != nil {
		return fmt.Errorf("persist accepted subagent %s: %w", task.ID, err)
	}
	return nil
}

// persistStatus synchronously updates status, result, iterations, and token
// counts. Callers that announce a terminal result must check the returned error
// before recording an announcement delivery state.
func (sm *SubagentManager) persistStatus(ctx context.Context, task *SubagentTask, iterations int) error {
	if sm.taskStore == nil || task.dbID == uuid.Nil {
		return nil
	}

	dbCtx := detachedCtx(ctx)

	sm.mu.RLock()
	snapshot := cloneSubagentTask(task)
	sm.mu.RUnlock()

	var result *string
	if snapshot.Result != "" {
		result = &snapshot.Result
	}

	if media := completionMediaDescriptors(
		snapshot.Media,
		snapshot.Workspace,
		snapshot.MediaPathPrefix,
	); len(media) > 0 {
		if err := retryTerminalPersistence(dbCtx, func(attemptCtx context.Context) error {
			return sm.taskStore.UpdateMetadata(attemptCtx, snapshot.RootAgentID, snapshot.dbID, map[string]any{
				asyncCompletionMediaKey: media,
			})
		}); err != nil {
			slog.Warn("subagent_persist: update completion media failed", "id", task.ID, "error", err)
			return err
		}
	}

	if err := retryTerminalPersistence(dbCtx, func(attemptCtx context.Context) error {
		return sm.taskStore.UpdateStatus(
			attemptCtx, snapshot.RootAgentID, snapshot.dbID,
			snapshot.Status, result, iterations,
			snapshot.TotalInputTokens, snapshot.TotalOutputTokens,
		)
	}); err != nil {
		slog.Warn("subagent_persist: update status failed", "id", task.ID, "error", err)
		return err
	}
	return nil
}

// UpdateAnnouncementStatus records whether the automatic parent resume message
// was delivered. A missed delivery is recoverable through spawn(action="get").
func (sm *SubagentManager) UpdateAnnouncementStatus(
	ctx context.Context,
	rootAgentID, completionID uuid.UUID,
	delivered bool,
) {
	if sm.taskStore == nil || rootAgentID == uuid.Nil || completionID == uuid.Nil {
		return
	}
	status := asyncCompletionDeliveryMissed
	if delivered {
		status = asyncCompletionDeliveryDone
	}
	dbCtx := detachedCtx(ctx)
	if err := retryAsyncPersistence(dbCtx, func(attemptCtx context.Context) error {
		return sm.taskStore.UpdateMetadata(attemptCtx, rootAgentID, completionID, map[string]any{
			asyncCompletionDeliveryKey: status,
		})
	}); err != nil {
		slog.Warn("subagent_persist: update announcement status failed",
			"completion_id", completionID,
			"root_agent_id", rootAgentID,
			"error", err,
		)
	}
}

// GetPersistedTask loads one durable self-clone result within the caller's
// tenant and root-agent scope. Delegate rows intentionally do not satisfy this
// contract.
func (sm *SubagentManager) GetPersistedTask(
	ctx context.Context,
	scope TaskScope,
	completionID uuid.UUID,
) (*store.SubagentTaskData, error) {
	if sm.taskStore == nil {
		return nil, fmt.Errorf("durable subagent task tracking is unavailable")
	}
	if scope.TenantID == uuid.Nil || scope.RootAgentID == uuid.Nil || completionID == uuid.Nil {
		return nil, fmt.Errorf("subagent completion lookup requires tenant, root agent, and completion ID")
	}
	dbCtx := store.WithTenantID(context.WithoutCancel(ctx), scope.TenantID)
	task, err := sm.taskStore.Get(dbCtx, scope.RootAgentID, completionID)
	if err != nil || task == nil {
		return task, err
	}
	if completionKind(task.Metadata) == asyncCompletionKindDelegate {
		return nil, nil
	}
	return task, nil
}

func completionKind(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[asyncCompletionKindKey].(string)
	return value
}
