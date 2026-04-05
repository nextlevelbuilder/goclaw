package tools

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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

// persistCreate writes a new subagent task to the DB (fire-and-forget).
func (sm *SubagentManager) persistCreate(ctx context.Context, task *SubagentTask) {
	if sm.taskStore == nil {
		return
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

	data := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: task.dbID},
		TenantID:       task.OriginTenantID,
		ParentAgentKey: task.ParentID,
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
	}

	if err := sm.taskStore.Create(dbCtx, data); err != nil {
		slog.Warn("subagent_persist: create failed", "id", task.ID, "error", err)
	}
}

// persistStatus updates status, result, iterations, and token counts in the DB (fire-and-forget).
func (sm *SubagentManager) persistStatus(ctx context.Context, task *SubagentTask, iterations int) {
	if sm.taskStore == nil || task.dbID == uuid.Nil {
		return
	}

	dbCtx := detachedCtx(ctx)

	var result *string
	if task.Result != "" {
		result = &task.Result
	}

	if err := sm.taskStore.UpdateStatus(
		dbCtx, task.dbID,
		task.Status, result, iterations,
		task.TotalInputTokens, task.TotalOutputTokens,
	); err != nil {
		slog.Warn("subagent_persist: update status failed", "id", task.ID, "error", err)
	}
}
