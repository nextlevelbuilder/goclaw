package methods

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ExecApprovalMethods handles exec.approval.list, exec.approval.approve, exec.approval.deny.
type ExecApprovalMethods struct {
	manager  *tools.ExecApprovalManager
	eventBus bus.EventPublisher
	store    store.ApprovalStore
}

func NewExecApprovalMethods(manager *tools.ExecApprovalManager, eventBus bus.EventPublisher) *ExecApprovalMethods {
	return &ExecApprovalMethods{manager: manager, eventBus: eventBus}
}

func (m *ExecApprovalMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodApprovalsList, m.handleList)
	router.Register(protocol.MethodApprovalsHistory, m.handleHistory)
	router.Register(protocol.MethodApprovalsApprove, m.handleApprove)
	router.Register(protocol.MethodApprovalsDeny, m.handleDeny)
}

// handleHistory returns the durable approval history for the caller's tenant
// (newest first), optionally filtered by status with limit/offset pagination.
// Returns an empty list when the durable store is not wired.
func (m *ExecApprovalMethods) handleHistory(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.store == nil {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"history": []any{},
			"total":   0,
		}))
		return
	}

	var params struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 50
	}
	if params.Offset < 0 {
		params.Offset = 0
	}

	items, err := m.store.ListHistory(ctx, client.TenantID(), store.ApprovalListOpts{
		Status: params.Status,
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgApprovalHistoryFailed, err.Error())))
		return
	}

	type historyInfo struct {
		ID           string `json:"id"`
		Command      string `json:"command"`
		ActionType   string `json:"actionType"`
		Status       string `json:"status"`
		Decision     string `json:"decision"`
		RequesterID  string `json:"requesterId,omitempty"`
		AgentID      string `json:"agentId,omitempty"`
		CreatedAt    int64  `json:"createdAt"`
		DecidedAt    int64  `json:"decidedAt,omitempty"`
	}
	out := make([]historyInfo, 0, len(items))
	for _, r := range items {
		hi := historyInfo{
			ID:         r.ID.String(),
			Command:    r.Command,
			ActionType: r.ActionType,
			Status:     r.Status,
			Decision:   r.Decision,
			CreatedAt:  r.CreatedAt.UnixMilli(),
		}
		if r.RequesterID != nil {
			hi.RequesterID = r.RequesterID.String()
		}
		if r.AgentID != nil {
			hi.AgentID = r.AgentID.String()
		}
		if r.DecidedAt != nil {
			hi.DecidedAt = r.DecidedAt.UnixMilli()
		}
		out = append(out, hi)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"history": out,
		"total":   len(out),
	}))
}

// SetApprovalStore wires the durable store for history reads. The manager owns
// persistence of the in-memory queue; the store here powers history/list
// fallback when the in-memory manager is empty (e.g. after a restart).
func (m *ExecApprovalMethods) SetApprovalStore(s store.ApprovalStore) { m.store = s }

func (m *ExecApprovalMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.manager == nil {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"pending": []any{},
		}))
		return
	}
	pending := m.manager.ListPending()

	type pendingInfo struct {
		ID        string `json:"id"`
		Command   string `json:"command"`
		AgentID   string `json:"agentId"`
		CreatedAt int64  `json:"createdAt"`
	}

	items := make([]pendingInfo, 0, len(pending))
	for _, pa := range pending {
		items = append(items, pendingInfo{
			ID:        pa.ID,
			Command:   pa.Command,
			AgentID:   pa.AgentID,
			CreatedAt: pa.CreatedAt.UnixMilli(),
		})
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"pending": items,
	}))
}

func (m *ExecApprovalMethods) handleApprove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.manager == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgExecApprovalDisabled)))
		return
	}

	var params struct {
		ID     string `json:"id"`
		Always bool   `json:"always"` // true = allow-always, false = allow-once
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}

	// Approver RBAC: approve/deny require operator+ (method policy already
	// checked RoleOperator at dispatch; re-verify for defense-in-depth).
	if !permissions.HasMinRole(client.Role(), permissions.RoleOperator) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "exec.approval.approve requires operator role")))
		return
	}

	decision := tools.ApprovalAllowOnce
	if params.Always {
		decision = tools.ApprovalAllowAlways
	}

	decidedBy := callerUUID(client)
	if err := m.manager.Resolve(ctx, params.ID, decision, decidedBy); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"resolved": true,
		"decision": string(decision),
	}))
	emitAudit(m.eventBus, client, "exec.approved", "exec", params.ID)
}

func (m *ExecApprovalMethods) handleDeny(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.manager == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgExecApprovalDisabled)))
		return
	}

	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}

	if !permissions.HasMinRole(client.Role(), permissions.RoleOperator) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "exec.approval.deny requires operator role")))
		return
	}

	decidedBy := callerUUID(client)
	if err := m.manager.Resolve(ctx, params.ID, tools.ApprovalDeny, decidedBy); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"resolved": true,
		"decision": "deny",
	}))
	emitAudit(m.eventBus, client, "exec.denied", "exec", params.ID)
}

// callerUUID resolves the WS client's identity to a UUID when it is a real
// tenant user (UUID-formatted). Non-UUID identities (channel user IDs) are
// recorded as nillable to keep decided_by typed.
func callerUUID(client *gateway.Client) *uuid.UUID {
	uid := client.UserID()
	parsed, err := uuid.Parse(uid)
	if err != nil {
		return nil
	}
	return &parsed
}
