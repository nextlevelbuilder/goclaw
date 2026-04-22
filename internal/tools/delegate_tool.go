package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DelegateResult carries the delegatee's response content and any media produced.
type DelegateResult struct {
	Content string
	Media   []bus.MediaFile
}

// DelegateRunFunc dispatches a delegation to a target agent.
// Injected by the gateway to avoid circular dependency with agent package.
// Returns the delegatee's response content + media, or error.
type DelegateRunFunc func(ctx context.Context, req DelegateRequest) (DelegateResult, error)

// DelegateRequest describes a delegation dispatch.
type DelegateRequest struct {
	FromAgentID uuid.UUID
	FromAgentKey string
	ToAgentKey   string
	Task         string
	DelegationID string
	UserID       string
	SenderID     string // real acting sender preserved through delegate announce re-ingress (#915)
	Role         string // caller's RBAC role; bypasses per-user grants for admin/operator/owner (#915)
	TenantID     string
	Channel      string
	ChatID       string
	PeerKind     string
	SessionKey   string
}

// DelegateTool implements the `delegate` tool for inter-agent task delegation.
// Uses existing agent_links infrastructure for permission checks.
type DelegateTool struct {
	links          store.AgentLinkStore
	agents         store.AgentCRUDStore
	eventBus       eventbus.DomainEventBus
	runFn          DelegateRunFunc
	msgBus         *bus.MessageBus  // for async announce back to parent
	hookDispatcher hooks.Dispatcher // optional; nil-safe
}

// SetMsgBus sets the message bus for async result delivery to parent agent.
func (t *DelegateTool) SetMsgBus(mb *bus.MessageBus) { t.msgBus = mb }

// SetHookDispatcher sets the hook dispatcher for SubagentStart/Stop events.
func (t *DelegateTool) SetHookDispatcher(d hooks.Dispatcher) { t.hookDispatcher = d }

// NewDelegateTool creates a delegate tool.
func NewDelegateTool(links store.AgentLinkStore, agents store.AgentCRUDStore, eb eventbus.DomainEventBus, runFn DelegateRunFunc) *DelegateTool {
	return &DelegateTool{links: links, agents: agents, eventBus: eb, runFn: runFn}
}

func (t *DelegateTool) Name() string { return "delegate" }

func (t *DelegateTool) Description() string {
	return "Delegate a task to a linked agent. The target agent must be connected via an agent link."
}

func (t *DelegateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_key": map[string]any{
				"type":        "string",
				"description": "The agent_key of the target agent to delegate to",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Description of the task to delegate",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"async", "sync"},
				"description": "async: fire-and-forget (default), sync: wait for completion",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds for sync mode (default: 300)",
			},
		},
		"required": []string{"agent_key", "task"},
	}
}

func (t *DelegateTool) Execute(ctx context.Context, args map[string]any) *Result {
	agentKey, _ := args["agent_key"].(string)
	task, _ := args["task"].(string)
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "async"
	}
	timeoutSec := 300
	if ts, ok := args["timeout"].(float64); ok && int(ts) > 0 {
		timeoutSec = int(ts)
	}
	if timeoutSec > 600 {
		timeoutSec = 600 // hard cap to prevent resource exhaustion
	}

	if agentKey == "" || task == "" {
		return ErrorResult("agent_key and task are required")
	}

	// Resolve calling agent from context
	fromAgentID := store.AgentIDFromContext(ctx)
	if fromAgentID == uuid.Nil {
		return ErrorResult("delegate requires agent context")
	}

	// Resolve target agent
	target, err := t.agents.GetByKey(ctx, agentKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("target agent %q not found", agentKey))
	}

	// Permission check via agent_links
	allowed, err := t.links.CanDelegate(ctx, fromAgentID, target.ID)
	if err != nil {
		slog.Warn("delegate.permission_check_error", "from", fromAgentID, "to", target.ID, "error", err)
		return ErrorResult("failed to check delegation permission")
	}
	if !allowed {
		return ErrorResult(fmt.Sprintf("no delegation link from current agent to %q", agentKey))
	}

	delegationID := uuid.New().String()
	// Audit-trail identity = actor (real sender). Groups audit actions to the
	// individual user rather than the group principal (#915).
	actorID := store.ActorIDFromContext(ctx)

	req := DelegateRequest{
		FromAgentID:  fromAgentID,
		FromAgentKey: store.AgentKeyFromContext(ctx),
		ToAgentKey:   agentKey,
		Task:         task,
		DelegationID: delegationID,
		UserID:       actorID,
		SenderID:     store.SenderIDFromContext(ctx),
		Role:         store.RoleFromContext(ctx),
		TenantID:     store.TenantIDFromContext(ctx).String(),
		Channel:      ToolChannelFromCtx(ctx),
		ChatID:       ToolChatIDFromCtx(ctx),
		PeerKind:     ToolPeerKindFromCtx(ctx),
		SessionKey:   ToolSessionKeyFromCtx(ctx),
	}

	// Emit delegate.sent event
	t.emitEvent(ctx, eventbus.EventDelegateSent, eventbus.DelegateSentPayload{
		DelegationID: delegationID,
		FromAgent:    req.FromAgentKey,
		ToAgent:      agentKey,
		Task:         task,
		Mode:         mode,
	})

	// Fire SubagentStart hook (blocking). Nil-safe: skip if no dispatcher.
	if t.hookDispatcher != nil {
		evt := hooks.Event{
			EventID:   uuid.NewString(),
			SessionID: req.SessionKey,
			TenantID:  parseUUIDOrNil(req.TenantID),
			AgentID:   req.FromAgentID,
			HookEvent: hooks.EventSubagentStart,
			Depth:     hooks.DepthFrom(ctx),
		}
		r, err := t.hookDispatcher.Fire(ctx, evt)
		if err != nil {
			t.emitEvent(ctx, eventbus.EventDelegateFailed, eventbus.DelegateFailedPayload{
				DelegationID: req.DelegationID,
				FromAgent:    req.FromAgentKey,
				ToAgent:      req.ToAgentKey,
				Error:        fmt.Sprintf("subagent_start hook error: %v", err),
			})
			return ErrorResult(fmt.Sprintf("subagent_start hook error: %v", err))
		}
		// Updated* from FireResult intentionally unused — delegate has no
		// mutation need in Wave 1.
		if r.Decision == hooks.DecisionBlock {
			t.emitEvent(ctx, eventbus.EventDelegateFailed, eventbus.DelegateFailedPayload{
				DelegationID: req.DelegationID,
				FromAgent:    req.FromAgentKey,
				ToAgent:      req.ToAgentKey,
				Error:        "blocked by subagent_start hook",
			})
			return ErrorResult(fmt.Sprintf("delegation to %q blocked by hook policy", req.ToAgentKey))
		}
		// Increment depth so nested delegate calls honor MaxLoopDepth.
		ctx = hooks.IncDepth(ctx)
	}

	if mode == "sync" {
		return t.executeSyncMode(ctx, req, timeoutSec)
	}
	return t.executeAsyncMode(ctx, req)
}

// executeSyncMode blocks until the delegatee completes or timeout.
func (t *DelegateTool) executeSyncMode(ctx context.Context, req DelegateRequest, timeoutSec int) *Result {
	syncCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	dr, err := t.runFn(syncCtx, req)
	if err != nil {
		t.emitEvent(ctx, eventbus.EventDelegateFailed, eventbus.DelegateFailedPayload{
			DelegationID: req.DelegationID,
			FromAgent:    req.FromAgentKey,
			ToAgent:      req.ToAgentKey,
			Error:        err.Error(),
		})
		return ErrorResult(fmt.Sprintf("delegation to %q failed: %v", req.ToAgentKey, err))
	}

	t.emitEvent(ctx, eventbus.EventDelegateCompleted, eventbus.DelegateCompletedPayload{
		DelegationID: req.DelegationID,
		FromAgent:    req.FromAgentKey,
		ToAgent:      req.ToAgentKey,
		Content:      truncate(dr.Content, 500),
		MediaCount:   len(dr.Media),
	})

	resultJSON, _ := json.Marshal(map[string]any{
		"delegation_id": req.DelegationID,
		"agent":         req.ToAgentKey,
		"status":        "completed",
		"content":       dr.Content,
	})
	r := NewResult(string(resultJSON))
	r.Media = dr.Media
	return r
}

// executeAsyncMode spawns a goroutine and returns immediately.
func (t *DelegateTool) executeAsyncMode(ctx context.Context, req DelegateRequest) *Result {
	// Detach from parent cancel but add a deadline to prevent goroutine leaks.
	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)

	go func() {
		defer cancel()
		dr, err := t.runFn(bgCtx, req)
		if err != nil {
			t.emitEvent(bgCtx, eventbus.EventDelegateFailed, eventbus.DelegateFailedPayload{
				DelegationID: req.DelegationID,
				FromAgent:    req.FromAgentKey,
				ToAgent:      req.ToAgentKey,
				Error:        err.Error(),
			})
			slog.Warn("delegate.async.failed", "to", req.ToAgentKey, "error", err)
			t.announceToParent(req, fmt.Sprintf("[Delegation to %s failed: %v]", req.ToAgentKey, err), nil)
			return
		}
		t.emitEvent(bgCtx, eventbus.EventDelegateCompleted, eventbus.DelegateCompletedPayload{
			DelegationID: req.DelegationID,
			FromAgent:    req.FromAgentKey,
			ToAgent:      req.ToAgentKey,
			Content:      truncate(dr.Content, 500),
			MediaCount:   len(dr.Media),
		})
		t.announceToParent(req, fmt.Sprintf("[Delegation result from %s]\n\n%s", req.ToAgentKey, dr.Content), dr.Media)
	}()

	result, _ := json.Marshal(map[string]any{
		"delegation_id": req.DelegationID,
		"agent":         req.ToAgentKey,
		"status":        "delegated",
		"message":       fmt.Sprintf("Task delegated to %s. You will be notified when complete.", req.ToAgentKey),
	})
	return NewResult(string(result))
}

// announceToParent delivers the delegate result back to the parent agent's
// conversation via msgBus, following the same pattern as subagent announce.
func (t *DelegateTool) announceToParent(req DelegateRequest, content string, media []bus.MediaFile) {
	if t.msgBus == nil || req.ChatID == "" {
		return
	}
	tenantUUID, _ := uuid.Parse(req.TenantID)
	meta := map[string]string{
		"origin_channel":     req.Channel,
		"origin_peer_kind":   req.PeerKind,
		"origin_session_key": req.SessionKey,
		"delegation_id":      req.DelegationID,
		"delegate_from":      req.FromAgentKey,
		"delegate_to":        req.ToAgentKey,
		MetaParentAgent:      req.FromAgentKey,
	}
	if req.SenderID != "" {
		meta[MetaOriginSenderID] = req.SenderID
	}
	if req.Role != "" {
		meta[MetaOriginRole] = req.Role
	}
	if req.UserID != "" {
		meta[MetaOriginUserID] = req.UserID
	}
	t.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: fmt.Sprintf("subagent:delegate:%s", req.DelegationID),
		ChatID:   req.ChatID,
		Content:  content,
		Media:    media,
		UserID:   req.UserID,
		TenantID: tenantUUID,
		Metadata: meta,
	})
}

// parseUUIDOrNil parses s as a UUID; returns uuid.Nil on failure.
func parseUUIDOrNil(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func (t *DelegateTool) emitEvent(ctx context.Context, eventType eventbus.EventType, payload any) {
	if t.eventBus == nil {
		return
	}
	t.eventBus.Publish(eventbus.DomainEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		TenantID:  store.TenantIDFromContext(ctx).String(),
		AgentID:   store.AgentIDFromContext(ctx).String(),
		UserID:    store.ActorIDFromContext(ctx), // audit actor, not scope (#915)
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})
}

