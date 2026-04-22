package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// HookMethods handles hooks.* RPC methods: list/create/update/delete/toggle/test/history.
// Test handler is an optional dependency; when nil, hooks.test returns a stub error.
type HookMethods struct {
	store   hooks.HookStore
	edition edition.Edition
	// TestRunner executes a HookConfig + sample Event without writing audit.
	// Typically wraps the dispatcher's handlers map for dry-run semantics.
	TestRunner HookTestRunner
}

// HookTestRunner runs a hook in dry-run mode. Implementations MUST NOT write
// to hook_executions; the WS layer returns the result directly to the caller.
type HookTestRunner interface {
	RunTest(ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) HookTestResult
}

// HookTestResult is the dry-run output surfaced to the Test panel UI.
type HookTestResult struct {
	Decision   hooks.Decision `json:"decision"`
	Reason     string         `json:"reason,omitempty"`
	DurationMS int            `json:"durationMs"`
	Stdout     string         `json:"stdout,omitempty"`
	Stderr     string         `json:"stderr,omitempty"`
	StatusCode int            `json:"statusCode,omitempty"`
	Error      string         `json:"error,omitempty"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
}

// NewHookMethods wires the methods with their store + edition context.
// Call SetTestRunner after construction to enable the `hooks.test` method.
func NewHookMethods(s hooks.HookStore, ed edition.Edition) *HookMethods {
	return &HookMethods{store: s, edition: ed}
}

// SetTestRunner attaches a runner for the `hooks.test` dry-run method.
// When unset, hooks.test returns an informative error.
func (m *HookMethods) SetTestRunner(r HookTestRunner) {
	m.TestRunner = r
}

func (m *HookMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodHooksList, m.requireViewer(m.handleList))
	router.Register(protocol.MethodHooksCreate, m.requireAdmin(m.handleCreate))
	router.Register(protocol.MethodHooksUpdate, m.requireAdmin(m.handleUpdate))
	router.Register(protocol.MethodHooksDelete, m.requireAdmin(m.handleDelete))
	router.Register(protocol.MethodHooksToggle, m.requireAdmin(m.handleToggle))
	router.Register(protocol.MethodHooksTest, m.requireOperator(m.handleTest))
	router.Register(protocol.MethodHooksHistory, m.requireViewer(m.handleHistory))
}

// ── RBAC middleware ────────────────────────────────────────────────────────

func (m *HookMethods) requireViewer(next gateway.MethodHandler) gateway.MethodHandler {
	return m.requireMinRole(permissions.RoleViewer, next)
}
func (m *HookMethods) requireOperator(next gateway.MethodHandler) gateway.MethodHandler {
	return m.requireMinRole(permissions.RoleOperator, next)
}
func (m *HookMethods) requireAdmin(next gateway.MethodHandler) gateway.MethodHandler {
	return m.requireMinRole(permissions.RoleAdmin, next)
}

func (m *HookMethods) requireMinRole(min permissions.Role, next gateway.MethodHandler) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		if !permissions.HasMinRole(client.Role(), min) {
			locale := store.LocaleFromContext(ctx)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized,
				i18n.T(locale, i18n.MsgPermissionDenied, req.Method)))
			return
		}
		next(ctx, client, req)
	}
}

// ── Handlers ───────────────────────────────────────────────────────────────

func (m *HookMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Event   string `json:"event"`
		Scope   string `json:"scope"`
		AgentID string `json:"agentId"`
		Enabled *bool  `json:"enabled"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	filter := hooks.ListFilter{Enabled: params.Enabled}
	if params.Event != "" {
		ev := hooks.HookEvent(params.Event)
		filter.Event = &ev
	}
	if params.Scope != "" {
		sc := hooks.Scope(params.Scope)
		filter.Scope = &sc
	}
	if params.AgentID != "" {
		if id, err := uuid.Parse(params.AgentID); err == nil {
			filter.AgentID = &id
		}
	}

	list, err := m.store.List(ctx, filter)
	if err != nil {
		locale := store.LocaleFromContext(ctx)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFailedToList, "hooks")+": "+err.Error()))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"hooks": list}))
}

func (m *HookMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	cfg, err := parseHookConfigParams(req.Params)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}

	// Master-scope guard for global hooks.
	if cfg.Scope == hooks.ScopeGlobal && !store.IsMasterScope(ctx) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgMasterScopeRequired)))
		return
	}
	// For tenant/agent scope, fill TenantID from ctx if not provided.
	if cfg.Scope != hooks.ScopeGlobal && cfg.TenantID == uuid.Nil {
		cfg.TenantID = store.TenantIDFromContext(ctx)
	}
	if cfg.Scope == hooks.ScopeGlobal {
		cfg.TenantID = hooks.SentinelTenantID
	}

	if err := cfg.Validate(m.edition); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	if uid := store.UserIDFromContext(ctx); uid != "" {
		if parsed, err := uuid.Parse(uid); err == nil {
			cfg.CreatedBy = &parsed
		}
	}

	id, err := m.store.Create(ctx, *cfg)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFailedToCreate, "hook", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"hookId": id.String()}))
}

func (m *HookMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		HookID  string         `json:"hookId"`
		Updates map[string]any `json:"updates"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	id, err := uuid.Parse(params.HookID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "hook")))
		return
	}
	if len(params.Updates) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidUpdates)))
		return
	}
	// Strip protected columns before any validation so callers cannot bypass
	// the scope/edition/matcher/timeout gate by sneaking them into updates.
	// `source` strip closes the C2 forge: without it a tenant admin could
	// PATCH {"source":"builtin"} on an existing UI row to escalate it into a
	// builtin-tier hook (which the dispatcher allows to mutate event input).
	// `created_by` strip prevents lying about provenance.
	delete(params.Updates, "id")
	delete(params.Updates, "tenant_id")
	delete(params.Updates, "version")
	delete(params.Updates, "source")
	delete(params.Updates, "created_by")

	// Re-validate the merged config: fetch current → apply patch → Validate.
	// Without this, admin-role callers could bypass edition gate, timeout
	// bounds, and prompt matcher/template invariants (C3 fix).
	current, err := m.store.GetByID(ctx, id)
	if err != nil || current == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgNotFound, "hook", params.HookID)))
		return
	}
	merged := applyHookPatch(*current, params.Updates)
	if verr := merged.Validate(m.edition); verr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, verr.Error()))
		return
	}

	if err := m.store.Update(ctx, id, params.Updates); err != nil {
		if errors.Is(err, hooks.ErrBuiltinReadOnly) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgHookBuiltinReadOnly)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFailedToUpdate, "hook", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"hookId": id.String()}))
}

// applyHookPatch produces the post-update config by overlaying the DB-column
// patch values onto cur. Only columns we actually expose via the form are
// recognized; unknown keys are tolerated (store layer will reject invalid
// columns). Used pre-validation so the Validate() contract runs on the
// full, merged config exactly as it will exist after Update commits.
func applyHookPatch(cur hooks.HookConfig, p map[string]any) hooks.HookConfig {
	if v, ok := p["name"].(string); ok {
		cur.Name = v
	}
	if v, ok := p["agent_ids"]; ok {
		if arr, ok := v.([]any); ok {
			var ids []uuid.UUID
			for _, item := range arr {
				if s, ok := item.(string); ok {
					if id, err := uuid.Parse(s); err == nil {
						ids = append(ids, id)
					}
				}
			}
			cur.AgentIDs = ids
		}
	}
	if v, ok := p["event"].(string); ok && v != "" {
		cur.Event = hooks.HookEvent(v)
	}
	if v, ok := p["scope"].(string); ok && v != "" {
		cur.Scope = hooks.Scope(v)
	}
	if v, ok := p["handler_type"].(string); ok && v != "" {
		cur.HandlerType = hooks.HandlerType(v)
	}
	if v, ok := p["matcher"].(string); ok {
		cur.Matcher = v
	}
	if v, ok := p["if_expr"].(string); ok {
		cur.IfExpr = v
	}
	if v, ok := p["timeout_ms"].(float64); ok {
		cur.TimeoutMS = int(v)
	}
	if v, ok := p["timeout_ms"].(int); ok {
		cur.TimeoutMS = v
	}
	if v, ok := p["on_timeout"].(string); ok && v != "" {
		cur.OnTimeout = hooks.Decision(v)
	}
	if v, ok := p["priority"].(float64); ok {
		cur.Priority = int(v)
	}
	if v, ok := p["priority"].(int); ok {
		cur.Priority = v
	}
	if v, ok := p["enabled"].(bool); ok {
		cur.Enabled = v
	}
	if v, ok := p["config"].(map[string]any); ok {
		cur.Config = v
	}
	if v, ok := p["metadata"].(map[string]any); ok {
		cur.Metadata = v
	}
	return cur
}

func (m *HookMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		HookID string `json:"hookId"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	id, err := uuid.Parse(params.HookID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "hook")))
		return
	}
	if err := m.store.Delete(ctx, id); err != nil {
		if errors.Is(err, hooks.ErrBuiltinReadOnly) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgHookBuiltinReadOnly)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFailedToDelete, "hook", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"hookId": id.String()}))
}

func (m *HookMethods) handleToggle(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		HookID  string `json:"hookId"`
		Enabled bool   `json:"enabled"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	id, err := uuid.Parse(params.HookID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "hook")))
		return
	}
	if err := m.store.Update(ctx, id, map[string]any{"enabled": params.Enabled}); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFailedToUpdate, "hook", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"hookId":  id.String(),
		"enabled": params.Enabled,
	}))
}

func (m *HookMethods) handleTest(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.TestRunner == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "test runner not configured")))
		return
	}
	var params struct {
		Config      json.RawMessage `json:"config"`
		SampleEvent json.RawMessage `json:"sampleEvent"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	cfg, err := parseHookConfigParams(params.Config)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}
	if cfg.Scope != hooks.ScopeGlobal && cfg.TenantID == uuid.Nil {
		cfg.TenantID = store.TenantIDFromContext(ctx)
	}
	if cfg.Scope == hooks.ScopeGlobal {
		cfg.TenantID = hooks.SentinelTenantID
	}
	if err := cfg.Validate(m.edition); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	ev, err := parseTestEventParams(params.SampleEvent, cfg)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}

	result := m.TestRunner.RunTest(ctx, *cfg, ev)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"result": result}))
}

func (m *HookMethods) handleHistory(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	// Phase 3 MVP stub — history reads are backed by hook_executions. Since
	// HookStore doesn't yet expose a paginated read API, we return an empty
	// list and a note so the UI can render a "not yet available" state while
	// Phase 4 wires the paginated reader.
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"executions": []any{},
		"nextCursor": "",
		"note":       "history pagination lands in phase 4",
	}))
	_ = ctx // reserved for future reader
}

// ── param helpers ─────────────────────────────────────────────────────────

func parseHookConfigParams(raw json.RawMessage) (*hooks.HookConfig, error) {
	if len(raw) == 0 {
		return nil, errors.New("missing config payload")
	}
	var cfg hooks.HookConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Metadata == nil {
		cfg.Metadata = map[string]any{}
	}
	if cfg.Config == nil {
		cfg.Config = map[string]any{}
	}
	if cfg.HandlerType == "" || cfg.Event == "" || cfg.Scope == "" {
		return nil, errors.New("handler_type, event, and scope are required")
	}
	// Strip caller-controlled identity / provenance fields. The dispatcher's
	// source-tier gate trusts `Source == "builtin"` to permit input mutation;
	// allowing a tenant admin to forge that here would let any script bypass
	// the readonly capability tier and rewrite event input. ID stripping keeps
	// the H9 cfg.ID-honor path (Phase 03) restricted to internal seeders only.
	cfg.Source = ""
	cfg.ID = uuid.Nil
	cfg.CreatedBy = nil
	cfg.Version = 0
	// Backward compat: singular agent_id → single-element AgentIDs.
	if len(cfg.AgentIDs) == 0 && cfg.AgentID != nil && *cfg.AgentID != uuid.Nil {
		cfg.AgentIDs = []uuid.UUID{*cfg.AgentID}
	}
	return &cfg, nil
}

func parseTestEventParams(raw json.RawMessage, cfg *hooks.HookConfig) (hooks.Event, error) {
	ev := hooks.Event{
		EventID:   fmt.Sprintf("test-%d", time.Now().UnixNano()),
		TenantID:  cfg.TenantID,
		HookEvent: cfg.Event,
	}
	if cfg.AgentID != nil {
		ev.AgentID = *cfg.AgentID
	}
	if len(raw) == 0 {
		return ev, nil
	}
	var sample struct {
		ToolName  string         `json:"toolName"`
		ToolInput map[string]any `json:"toolInput"`
		RawInput  string         `json:"rawInput"`
	}
	if err := json.Unmarshal(raw, &sample); err != nil {
		return ev, fmt.Errorf("invalid sampleEvent: %w", err)
	}
	ev.ToolName = sample.ToolName
	ev.ToolInput = sample.ToolInput
	ev.RawInput = sample.RawInput
	return ev, nil
}
