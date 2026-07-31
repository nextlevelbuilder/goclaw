package methods

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workflow"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkflowsMethods serves the authored-workflow CRUD surface.
//
// Every handler resolves the tenant from the CONNECTION's context, never from
// params. A tenant id in the payload would be a request to act as someone else,
// and the store's isolation only helps if the caller cannot choose the scope.
type WorkflowsMethods struct {
	workflows store.WorkflowStore
	// compiler turns an armed graph into cron jobs. Nil-safe: without it the CRUD
	// surface still works and workflows simply never arm, which is the honest
	// behaviour for a deployment with no scheduler rather than pretending to save
	// something that will fire.
	compiler *workflow.Compiler
}

func NewWorkflowsMethods(workflows store.WorkflowStore, compiler *workflow.Compiler) *WorkflowsMethods {
	return &WorkflowsMethods{workflows: workflows, compiler: compiler}
}

func (m *WorkflowsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodWorkflowsList, m.handleList)
	router.Register(protocol.MethodWorkflowsGet, m.handleGet)
	router.Register(protocol.MethodWorkflowsCreate, m.handleCreate)
	router.Register(protocol.MethodWorkflowsUpdate, m.handleUpdate)
	router.Register(protocol.MethodWorkflowsDelete, m.handleDelete)
	router.Register(protocol.MethodWorkflowsSetEnabled, m.handleSetEnabled)
}

// maxGraphBytes caps an authored graph. A canvas graph is small (tens of nodes);
// anything approaching this is a bug or an attempt to use the row as storage, and
// an unbounded JSONB column is a cheap way to fill a tenant's disk.
const maxGraphBytes = 512 * 1024

// workflowInfo is the wire shape. camelCase, matching the other WS DTOs — note
// the HTTP DTOs in this codebase are snake_case, and mixing them up has already
// caused one bug (see connectionCap in the website).
type workflowInfo struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Enabled      bool            `json:"enabled"`
	Graph        json.RawMessage `json:"graph"`
	Compiled     json.RawMessage `json:"compiled,omitempty"`
	CompileError string          `json:"compileError,omitempty"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

func toWorkflowInfo(w *store.Workflow) workflowInfo {
	info := workflowInfo{
		ID:        w.ID.String(),
		Name:      w.Name,
		Enabled:   w.Enabled,
		Graph:     w.Graph,
		Compiled:  w.Compiled,
		CreatedAt: w.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: w.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if w.Description != nil {
		info.Description = *w.Description
	}
	if w.CompileError != nil {
		info.CompileError = *w.CompileError
	}
	if len(info.Graph) == 0 {
		info.Graph = json.RawMessage(`{}`)
	}
	return info
}

// scope resolves the tenant and reports whether the request can proceed.
func (m *WorkflowsMethods) scope(ctx context.Context, client *gateway.Client, reqID string) (uuid.UUID, bool) {
	locale := store.LocaleFromContext(ctx)
	if m.workflows == nil {
		client.SendResponse(protocol.NewErrorResponse(reqID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "workflows are not available on this deployment")))
		return uuid.Nil, false
	}
	tenant := store.TenantIDFromContext(ctx)
	if tenant == uuid.Nil {
		// Refused rather than treated as a global scope: a nil tenant here would
		// make one caller's workflows visible to everyone.
		client.SendResponse(protocol.NewErrorResponse(reqID, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgPermissionDenied, "workflows")))
		return uuid.Nil, false
	}
	return tenant, true
}

func (m *WorkflowsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenant, ok := m.scope(ctx, client, req.ID)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(ctx)
	rows, err := m.workflows.ListForTenant(ctx, tenant)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "workflows")))
		return
	}
	items := make([]workflowInfo, 0, len(rows))
	for _, w := range rows {
		items = append(items, toWorkflowInfo(w))
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workflows": items}))
}

func (m *WorkflowsMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenant, ok := m.scope(ctx, client, req.ID)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(ctx)
	id, ok := m.parseID(ctx, client, req)
	if !ok {
		return
	}
	w, err := m.workflows.Get(ctx, tenant, id)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "workflow")))
		return
	}
	if w == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "workflow")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workflow": toWorkflowInfo(w)}))
}

type workflowWriteParams struct {
	ID          string          `json:"id"`
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Graph       json.RawMessage `json:"graph"`
	Enabled     *bool           `json:"enabled"`
}

func (m *WorkflowsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenant, ok := m.scope(ctx, client, req.ID)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(ctx)
	var p workflowWriteParams
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &p)
	}
	name := strings.TrimSpace(derefString(p.Name))
	if name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if msg, bad := validateGraph(p.Graph); bad {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, msg))
		return
	}

	w := newWorkflowFromParams(tenant, store.UserIDFromContext(ctx), name, p)

	if err := m.workflows.Create(ctx, w); err != nil {
		if errors.Is(err, store.ErrWorkflowNameTaken) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidRequest, "a workflow with that name already exists")))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "workflow", "internal error")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workflow": toWorkflowInfo(w)}))
}

// newWorkflowFromParams maps create params onto a row.
//
// A pure function so the one invariant that matters here is testable without
// standing up a transport: a workflow is NEVER born armed. Enabled is ignored
// even when the client sends it, because arming is workflows.setEnabled — the
// difference between "I saved a draft" and "I started something that runs
// unattended" should not be a boolean that a client can round-trip by accident.
func newWorkflowFromParams(tenant uuid.UUID, userID, name string, p workflowWriteParams) *store.Workflow {
	w := &store.Workflow{
		TenantID: tenant,
		Name:     name,
		Graph:    p.Graph,
		Enabled:  false,
	}
	if d := strings.TrimSpace(derefString(p.Description)); d != "" {
		w.Description = &d
	}
	if uid := strings.TrimSpace(userID); uid != "" {
		w.CreatedBy = &uid
	}
	return w
}

func (m *WorkflowsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenant, ok := m.scope(ctx, client, req.ID)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(ctx)
	var p workflowWriteParams
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &p)
	}
	id, err := uuid.Parse(strings.TrimSpace(p.ID))
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}
	if msg, bad := validateGraph(p.Graph); bad {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, msg))
		return
	}

	// Read-modify-write against the CURRENT row so an omitted field keeps its
	// value. A blind write would let a client that knows only about `graph` erase
	// the name and description it never sent.
	existing, err := m.workflows.Get(ctx, tenant, id)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "workflow")))
		return
	}
	if existing == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "workflow")))
		return
	}
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
			return
		}
		existing.Name = name
	}
	if p.Description != nil {
		d := strings.TrimSpace(*p.Description)
		existing.Description = &d
	}
	if len(p.Graph) > 0 {
		existing.Graph = p.Graph
	}
	// Enabled is intentionally ignored here even if sent — see setEnabled.

	if err := m.workflows.Update(ctx, existing); err != nil {
		if errors.Is(err, store.ErrWorkflowNameTaken) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidRequest, "a workflow with that name already exists")))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "workflow", "internal error")))
		return
	}
	// Recompile after every save, not only when armed. Editing an ARMED workflow
	// must change what fires — otherwise the graph on screen and the schedule
	// running disagree, silently. For a disarmed one this is a cheap no-op that
	// also cleans up anything a previous arm left behind.
	m.recompile(ctx, existing)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workflow": toWorkflowInfo(existing)}))
}

// handleSetEnabled arms or disarms a workflow.
//
// Separate from update on purpose: "save my edits" and "start firing this" are
// different acts with different consequences, and a boolean inside a patch is too
// easy to send by accident from a client that round-trips a whole object.
func (m *WorkflowsMethods) handleSetEnabled(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenant, ok := m.scope(ctx, client, req.ID)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(ctx)
	var p struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &p)
	}
	id, err := uuid.Parse(strings.TrimSpace(p.ID))
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}
	if p.Enabled == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "enabled")))
		return
	}

	existing, err := m.workflows.Get(ctx, tenant, id)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "workflow")))
		return
	}
	if existing == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "workflow")))
		return
	}
	existing.Enabled = *p.Enabled
	if err := m.workflows.Update(ctx, existing); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "workflow", "internal error")))
		return
	}
	// Arm (or disarm) NOW rather than leaving it to the next reconcile: a user who
	// presses Arm and sees success must not have to wait for a restart before
	// anything fires. Apply writes any compile error onto `existing`, so the
	// response below carries the reason a graph refused to arm.
	m.recompile(ctx, existing)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workflow": toWorkflowInfo(existing)}))
}

func (m *WorkflowsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenant, ok := m.scope(ctx, client, req.ID)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(ctx)
	id, ok := m.parseID(ctx, client, req)
	if !ok {
		return
	}
	// Retract BEFORE deleting: the row carries the only record of which cron jobs
	// this workflow created, so deleting first would strand them with nothing left
	// to explain where they came from.
	if m.compiler != nil && m.compiler.Available() {
		if existing, err := m.workflows.Get(ctx, tenant, id); err == nil && existing != nil {
			m.compiler.Retract(ctx, existing)
		} else if err != nil {
			// Refuse rather than delete blind — an orphaned schedule that fires
			// forever is worse than a delete the user has to retry.
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "workflow", "internal error")))
			return
		}
	}
	if err := m.workflows.Delete(ctx, tenant, id); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "workflow", "internal error")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"deleted": true}))
}

func (m *WorkflowsMethods) parseID(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) (uuid.UUID, bool) {
	locale := store.LocaleFromContext(ctx)
	var p struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &p)
	}
	id, err := uuid.Parse(strings.TrimSpace(p.ID))
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return uuid.Nil, false
	}
	return id, true
}

// recompile applies the compiler, tolerating its absence. A compile FAILURE is
// not an error for the caller: it is recorded on the row and returned in the
// response as compileError, because "your schedule is invalid" is feedback about
// the graph, not a failed request.
func (m *WorkflowsMethods) recompile(ctx context.Context, w *store.Workflow) {
	if m.compiler == nil || !m.compiler.Available() {
		return
	}
	if err := m.compiler.Apply(ctx, w); err != nil {
		slog.Error("workflows.compile_apply_failed", "workflow", w.ID, "error", err)
	}
}

// validateGraph checks the little the API can check. Structural validity is the
// compiler's job (the DB stores JSONB opaquely by design), but a graph that is
// not even a JSON OBJECT, or is large enough to be an attack, is rejected at the
// door rather than stored and failed later.
func validateGraph(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	if len(raw) > maxGraphBytes {
		return "graph is too large", true
	}
	if !json.Valid(raw) {
		return "graph is not valid JSON", true
	}
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") {
		return "graph must be a JSON object", true
	}
	return "", false
}
