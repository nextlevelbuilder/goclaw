package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const delegateCompletionTargetKey = "target_agent_key"

const delegateRunningPersistenceTimeout = 250 * time.Millisecond

func (t *DelegateTool) createDelegateCompletion(ctx context.Context, req DelegateRequest) error {
	if t.taskStore == nil {
		return nil
	}
	delegationID, err := uuid.Parse(req.DelegationID)
	if err != nil {
		return fmt.Errorf("invalid delegation ID: %w", err)
	}
	tenantID := parseUUIDOrNil(req.TenantID)
	if tenantID == uuid.Nil || req.FromAgentID == uuid.Nil {
		return fmt.Errorf("delegation completion requires tenant and source agent")
	}
	var sessionKey, originChannel, originChatID, originPeerKind, originUserID *string
	if req.SessionKey != "" {
		sessionKey = &req.SessionKey
	}
	if req.Channel != "" {
		originChannel = &req.Channel
	}
	if req.ChatID != "" {
		originChatID = &req.ChatID
	}
	if req.PeerKind != "" {
		originPeerKind = &req.PeerKind
	}
	if req.UserID != "" {
		originUserID = &req.UserID
	}
	data := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: delegationID},
		TenantID:       tenantID,
		RootAgentID:    req.FromAgentID,
		ParentAgentKey: req.FromAgentKey,
		SessionKey:     sessionKey,
		Subject:        "Delegate to " + req.ToAgentKey,
		Description:    req.Task,
		Status:         TaskStatusQueued,
		Depth:          subagentDepthFromContext(ctx, 0) + 1,
		OriginChannel:  originChannel,
		OriginChatID:   originChatID,
		OriginPeerKind: originPeerKind,
		OriginUserID:   originUserID,
		Metadata: map[string]any{
			asyncCompletionKindKey:      asyncCompletionKindDelegate,
			delegateCompletionTargetKey: req.ToAgentKey,
			asyncCompletionDeliveryKey:  asyncCompletionDeliveryPending,
		},
	}
	dbCtx := store.WithTenantID(context.Background(), tenantID)
	if err := retryAsyncPersistence(dbCtx, func(attemptCtx context.Context) error {
		return t.taskStore.Create(attemptCtx, data)
	}); err != nil {
		return fmt.Errorf("persist accepted delegation %s: %w", req.DelegationID, err)
	}
	return nil
}

func (t *DelegateTool) updateDelegateCompletion(
	req DelegateRequest,
	status string,
	result *string,
) error {
	if t.taskStore == nil {
		return nil
	}
	delegationID := parseUUIDOrNil(req.DelegationID)
	tenantID := parseUUIDOrNil(req.TenantID)
	if delegationID == uuid.Nil || tenantID == uuid.Nil || req.FromAgentID == uuid.Nil {
		return fmt.Errorf("delegation completion requires valid tenant, source agent, and delegation ID")
	}
	dbCtx := store.WithTenantID(context.Background(), tenantID)
	if err := retryTerminalPersistence(dbCtx, func(attemptCtx context.Context) error {
		return t.taskStore.UpdateStatus(
			attemptCtx,
			req.FromAgentID,
			delegationID,
			status,
			result,
			0,
			0,
			0,
		)
	}); err != nil {
		slog.Error("delegate.async.completion_persist_failed",
			"delegation_id", req.DelegationID,
			"from_agent_id", req.FromAgentID,
			"status", status,
			"error", err,
		)
		return err
	}
	return nil
}

// updateDelegateRunning is observability-only: execution may proceed even when
// this transition cannot be persisted. Keep it to one short attempt outside
// the admission callback so a database outage cannot monopolize a child-run
// permit.
func (t *DelegateTool) updateDelegateRunning(req DelegateRequest) error {
	if t.taskStore == nil {
		return nil
	}
	delegationID := parseUUIDOrNil(req.DelegationID)
	tenantID := parseUUIDOrNil(req.TenantID)
	if delegationID == uuid.Nil || tenantID == uuid.Nil || req.FromAgentID == uuid.Nil {
		return fmt.Errorf("delegation completion requires valid tenant, source agent, and delegation ID")
	}
	dbCtx, cancel := context.WithTimeout(
		store.WithTenantID(context.Background(), tenantID),
		delegateRunningPersistenceTimeout,
	)
	defer cancel()
	err := t.taskStore.UpdateStatus(
		dbCtx,
		req.FromAgentID,
		delegationID,
		TaskStatusRunning,
		nil,
		0,
		0,
		0,
	)
	if err != nil {
		slog.Warn("delegate.async.running_persist_failed",
			"delegation_id", req.DelegationID,
			"from_agent_id", req.FromAgentID,
			"error", err,
		)
	}
	return err
}

func (t *DelegateTool) updateDelegateCompletionMedia(
	req DelegateRequest,
	media []persistedCompletionMedia,
) error {
	if t.taskStore == nil || len(media) == 0 {
		return nil
	}
	delegationID := parseUUIDOrNil(req.DelegationID)
	tenantID := parseUUIDOrNil(req.TenantID)
	if delegationID == uuid.Nil || tenantID == uuid.Nil || req.FromAgentID == uuid.Nil {
		return fmt.Errorf("delegation completion requires valid tenant, source agent, and delegation ID")
	}
	dbCtx := store.WithTenantID(context.Background(), tenantID)
	if err := retryTerminalPersistence(dbCtx, func(attemptCtx context.Context) error {
		return t.taskStore.UpdateMetadata(attemptCtx, req.FromAgentID, delegationID, map[string]any{
			asyncCompletionMediaKey: media,
		})
	}); err != nil {
		slog.Error("delegate.async.completion_media_persist_failed",
			"delegation_id", req.DelegationID,
			"from_agent_id", req.FromAgentID,
			"error", err,
		)
		return err
	}
	return nil
}

func (t *DelegateTool) updateDelegateAnnouncement(req DelegateRequest, delivered bool) {
	if t.taskStore == nil {
		return
	}
	delegationID := parseUUIDOrNil(req.DelegationID)
	tenantID := parseUUIDOrNil(req.TenantID)
	if delegationID == uuid.Nil || tenantID == uuid.Nil || req.FromAgentID == uuid.Nil {
		return
	}
	status := asyncCompletionDeliveryMissed
	if delivered {
		status = asyncCompletionDeliveryDone
	}
	dbCtx := store.WithTenantID(context.Background(), tenantID)
	if err := retryAsyncPersistence(dbCtx, func(attemptCtx context.Context) error {
		return t.taskStore.UpdateMetadata(attemptCtx, req.FromAgentID, delegationID, map[string]any{
			asyncCompletionDeliveryKey: status,
		})
	}); err != nil {
		slog.Warn("delegate.async.announcement_status_failed",
			"delegation_id", req.DelegationID,
			"error", err,
		)
	}
}

func (t *DelegateTool) executeGetCompletion(ctx context.Context, args map[string]any) *Result {
	rawID, _ := args["delegation_id"].(string)
	delegationID, err := uuid.Parse(rawID)
	if err != nil {
		return ErrorResult("delegation_id must be a valid UUID")
	}
	tenantID := store.TenantIDFromContext(ctx)
	fromAgentID := store.AgentIDFromContext(ctx)
	if tenantID == uuid.Nil || fromAgentID == uuid.Nil {
		return ErrorResult("delegate result lookup requires tenant and agent context")
	}
	if t.taskStore == nil {
		return ErrorResult("durable delegation task tracking is unavailable")
	}
	dbCtx := store.WithTenantID(context.WithoutCancel(ctx), tenantID)
	task, err := t.taskStore.Get(dbCtx, fromAgentID, delegationID)
	if err != nil {
		return ErrorResult("failed to load delegation result")
	}
	if task == nil || completionKind(task.Metadata) != asyncCompletionKindDelegate {
		return ErrorResult("delegation result not found")
	}
	payload := persistedCompletionPayload(task)
	payload["delegation_id"] = task.ID.String()
	delete(payload, "completion_id")
	if target, ok := task.Metadata[delegateCompletionTargetKey].(string); ok && target != "" {
		payload["agent"] = target
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult("failed to encode delegation result")
	}
	return NewResult(string(encoded))
}
