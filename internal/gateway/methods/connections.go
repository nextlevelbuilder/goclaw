package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ConnectionsMethods handles connections.list / create / update / delete and
// connections.credential.set / .delete — the WS twin of the /v1/connections HTTP
// surface for the TENANT-LEVEL CLI connection catalogue (migration 000082).
//
// Scoping rules, identical to the HTTP handlers:
//   - reads return the caller's tenant rows plus global (tenant_id NULL) rows;
//   - a tenant may never read or mutate another tenant's row (the tenant comes
//     from the connection context, never from client params);
//   - global rows are platform state: mutating one requires master scope;
//   - credentials are scoped to the ACTING USER and NEVER returned — only their
//     presence and type.
type ConnectionsMethods struct {
	conns    store.CLIConnectionStore
	eventBus bus.EventPublisher
	// cliChat serves connections.chat.open's availability check. Nil-safe.
	cliChat *CLIChat
}

func NewConnectionsMethods(conns store.CLIConnectionStore, eventBus bus.EventPublisher) *ConnectionsMethods {
	return &ConnectionsMethods{conns: conns, eventBus: eventBus}
}

func (m *ConnectionsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodConnectionsList, m.handleList)
	router.Register(protocol.MethodConnectionsCreate, m.handleCreate)
	router.Register(protocol.MethodConnectionsUpdate, m.handleUpdate)
	router.Register(protocol.MethodConnectionsDelete, m.handleDelete)
	router.Register(protocol.MethodConnectionsCredentialSet, m.handleCredentialSet)
	router.Register(protocol.MethodConnectionsCredentialDelete, m.handleCredentialDelete)
	router.Register(protocol.MethodConnectionsChatOpen, m.handleChatOpen)
}

// SetCLIChat wires the interactive-CLI layer so connections.chat.open can report
// honestly whether this deployment can serve a conversation at all. Nil-safe:
// without it the method still validates the connection, then says the feature is
// unavailable rather than handing back a session key that could never work.
func (m *ConnectionsMethods) SetCLIChat(c *CLIChat) { m.cliChat = c }

// handleChatOpen opens an interactive conversation with a connected coding CLI.
//
// It mints nothing but a session key — the CLI process starts on the first
// chat.send. What it DOES do is refuse, up front and with the reason, every case
// that could not work: a connection the caller's tenant cannot see, a disabled
// one, one whose provider cannot be run at all, and one whose provider has no
// interactive mode. That last check is the point of the method: silently opening
// a session against a CLI with no stdin protocol would swallow every message the
// user then sent.
func (m *ConnectionsMethods) handleChatOpen(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if !m.available(ctx, client, req.ID) {
		return
	}
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ConnectionID string `json:"connectionId"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
			return
		}
	}
	id, err := uuid.Parse(strings.TrimSpace(params.ConnectionID))
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "connectionId")))
		return
	}
	// Tenant-scoped: another tenant's row is indistinguishable from absent.
	conn := m.resolve(ctx, client, req.ID, id)
	if conn == nil {
		return
	}
	if !conn.Enabled {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, conn.Name+" is disabled — enable it under Connections first")))
		return
	}

	// Resolve the invocation and report the provider's interactive support
	// honestly, naming the provider so the message is actionable.
	spec, err := cliagent.Resolve(conn.Provider, conn.Config)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, conn.Name+" cannot be run: "+err.Error())))
		return
	}
	if !spec.SupportsInteractive() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, cliNoInteractiveDetail(conn.Name, conn.Provider))))
		return
	}

	// Last: whether this deployment can actually serve the conversation. Checked
	// after the connection-specific validation so the caller always gets the most
	// specific reason.
	if m.cliChat == nil || !m.cliChat.Ready() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "interactive CLI chat is not available on this deployment (it needs the connection catalogue and a sandbox)")))
		return
	}

	sessionKey := sessions.BuildCLISessionKey(conn.ID.String(), uuid.NewString())
	slog.Info("connections.chat.open", "connection", conn.ID, "provider", conn.Provider,
		"permission_mode", string(cliagent.PermissionModeFromConfig(conn.Config)), "user_id", client.UserID())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"sessionKey":   sessionKey,
		"connectionId": conn.ID.String(),
		"provider":     cliagent.CanonicalProvider(conn.Provider),
		// The mode the conversation will run in, so the UI can say whether the
		// user will be asked to approve each action or not.
		"permissionMode": string(cliagent.PermissionModeFromConfig(conn.Config)),
	}))
}

// connectionParams is the union of every mutation's params. Params are camelCase
// per the WS convention; `id` identifies the connection for update/delete and
// credential.*.
type connectionParams struct {
	ID       string           `json:"id"`
	Name     *string          `json:"name"`
	Kind     *string          `json:"kind"`
	Provider *string          `json:"provider"`
	Mode     *string          `json:"mode"`
	Endpoint *string          `json:"endpoint"`
	Enabled  *bool            `json:"enabled"`
	Config   *json.RawMessage `json:"config"`
	// Credential fields (connections.credential.set only).
	Type   string `json:"type"`
	Secret string `json:"secret"`
}

// connectionInfo is the wire shape of a connection: no secrets, plus whether the
// acting user resolves to a credential.
type connectionInfo struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenantId,omitempty"`
	Global         bool            `json:"global"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Provider       string          `json:"provider"`
	Mode           string          `json:"mode"`
	Endpoint       string          `json:"endpoint,omitempty"`
	Enabled        bool            `json:"enabled"`
	Config         json.RawMessage `json:"config,omitempty"`
	CreatedBy      string          `json:"createdBy,omitempty"`
	CreatedAt      int64           `json:"createdAt"`
	UpdatedAt      int64           `json:"updatedAt"`
	HasCredential  bool            `json:"hasCredential"`
	CredentialType string          `json:"credentialType,omitempty"`
}

func toConnectionInfo(c store.CLIConnection) connectionInfo {
	info := connectionInfo{
		ID:        c.ID.String(),
		Global:    c.TenantID == nil,
		Name:      c.Name,
		Kind:      c.Kind,
		Provider:  c.Provider,
		Mode:      c.Mode,
		Endpoint:  c.Endpoint,
		Enabled:   c.Enabled,
		Config:    c.Config,
		CreatedBy: c.CreatedBy,
		CreatedAt: c.CreatedAt.UnixMilli(),
		UpdatedAt: c.UpdatedAt.UnixMilli(),
	}
	if c.TenantID != nil {
		info.TenantID = c.TenantID.String()
	}
	return info
}

// tenantScopeFromCtx returns the caller's tenant as the pointer the store wants.
// nil = platform scope: reads see only global rows, writes target global rows.
func tenantScopeFromCtx(ctx context.Context) *uuid.UUID {
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		return &tid
	}
	return nil
}

// parseConnectionParams decodes params and, when required, the connection id.
// On failure it responds and returns ok=false.
func parseConnectionParams(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, needID bool) (connectionParams, uuid.UUID, bool) {
	locale := store.LocaleFromContext(ctx)
	var params connectionParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
			return params, uuid.Nil, false
		}
	}
	if !needID {
		return params, uuid.Nil, true
	}
	id, err := uuid.Parse(strings.TrimSpace(params.ID))
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "connection")))
		return params, uuid.Nil, false
	}
	return params, id, true
}

// resolve loads a connection visible to the caller's tenant. On failure it
// responds and returns nil.
func (m *ConnectionsMethods) resolve(ctx context.Context, client *gateway.Client, reqID string, id uuid.UUID) *store.CLIConnection {
	locale := store.LocaleFromContext(ctx)
	conn, err := m.conns.GetByID(ctx, tenantScopeFromCtx(ctx), id)
	if err != nil {
		slog.Error("connections.get", "id", id, "error", err)
		client.SendResponse(protocol.NewErrorResponse(reqID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "connection")))
		return nil
	}
	if conn == nil {
		// Invisible is indistinguishable from absent — never confirm that
		// another tenant's row exists.
		client.SendResponse(protocol.NewErrorResponse(reqID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "connection", id.String())))
		return nil
	}
	return conn
}

// requireOwningScope rejects a mutation of a GLOBAL row by a tenant-scoped
// caller (same rule as http.requireMasterScope). Tenant rows are already proven
// to belong to the caller's tenant by resolve().
func (m *ConnectionsMethods) requireOwningScope(ctx context.Context, client *gateway.Client, reqID string, conn *store.CLIConnection) bool {
	if conn.TenantID != nil || store.IsMasterScope(ctx) {
		return true
	}
	slog.Warn("security.tenant_scope_violation",
		"method", "connections.*", "connection", conn.ID,
		"tenant_id", store.TenantIDFromContext(ctx), "user_id", client.UserID())
	locale := store.LocaleFromContext(ctx)
	client.SendResponse(protocol.NewErrorResponse(reqID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgMasterScopeRequired)))
	return false
}

func (m *ConnectionsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.conns == nil {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"connections": []any{},
		}))
		return
	}
	locale := store.LocaleFromContext(ctx)
	conns, err := m.conns.ListForTenant(ctx, tenantScopeFromCtx(ctx), false)
	if err != nil {
		slog.Error("connections.list", "tenant_id", store.TenantIDFromContext(ctx), "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "connections")))
		return
	}

	userID := client.UserID()
	items := make([]connectionInfo, 0, len(conns))
	for _, c := range conns {
		info := toConnectionInfo(c)
		// Presence + type only; the secret itself never leaves the store.
		if userID != "" {
			if cred, err := m.conns.GetCredential(ctx, c.ID, userID); err == nil && cred != nil {
				info.HasCredential = true
				info.CredentialType = cred.Type
			}
		}
		items = append(items, info)
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"connections": items,
	}))
}

func (m *ConnectionsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if !m.available(ctx, client, req.ID) {
		return
	}
	locale := store.LocaleFromContext(ctx)
	params, _, ok := parseConnectionParams(ctx, client, req, false)
	if !ok {
		return
	}
	name := strings.TrimSpace(derefString(params.Name))
	if name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	provider := strings.ToLower(strings.TrimSpace(derefString(params.Provider)))
	if provider == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "provider")))
		return
	}

	tenantID := tenantScopeFromCtx(ctx)
	// tenant_id NULL is a platform-wide row — RoleAdmin inside one tenant is not
	// enough to create one.
	if tenantID == nil && !store.IsMasterScope(ctx) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgMasterScopeRequired)))
		return
	}

	conn := &store.CLIConnection{
		TenantID:  tenantID,
		Name:      name,
		Kind:      firstNonEmptyStr(strings.TrimSpace(derefString(params.Kind)), "external_cli"),
		Provider:  provider,
		Mode:      firstNonEmptyStr(strings.TrimSpace(derefString(params.Mode)), "delegate"),
		Endpoint:  strings.TrimSpace(derefString(params.Endpoint)),
		Enabled:   params.Enabled == nil || *params.Enabled,
		CreatedBy: client.UserID(),
	}
	if params.Config != nil {
		conn.Config = *params.Config
	}
	if err := m.conns.Upsert(ctx, conn); err != nil {
		slog.Error("connections.create", "name", name, "provider", provider, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "connection", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"connection": toConnectionInfo(*conn),
	}))
	emitAudit(m.eventBus, client, "connection.created", "cli_connection", conn.ID.String())
}

func (m *ConnectionsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if !m.available(ctx, client, req.ID) {
		return
	}
	locale := store.LocaleFromContext(ctx)
	params, id, ok := parseConnectionParams(ctx, client, req, true)
	if !ok {
		return
	}
	conn := m.resolve(ctx, client, req.ID, id)
	if conn == nil {
		return
	}
	if !m.requireOwningScope(ctx, client, req.ID, conn) {
		return
	}

	if params.Name != nil {
		name := strings.TrimSpace(*params.Name)
		if name == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
			return
		}
		conn.Name = name
	}
	if params.Kind != nil && strings.TrimSpace(*params.Kind) != "" {
		conn.Kind = strings.TrimSpace(*params.Kind)
	}
	if params.Provider != nil && strings.TrimSpace(*params.Provider) != "" {
		conn.Provider = strings.ToLower(strings.TrimSpace(*params.Provider))
	}
	if params.Mode != nil && strings.TrimSpace(*params.Mode) != "" {
		conn.Mode = strings.TrimSpace(*params.Mode)
	}
	if params.Endpoint != nil {
		conn.Endpoint = strings.TrimSpace(*params.Endpoint)
	}
	if params.Enabled != nil {
		conn.Enabled = *params.Enabled
	}
	if params.Config != nil {
		conn.Config = *params.Config
	}

	if err := m.conns.Upsert(ctx, conn); err != nil {
		slog.Error("connections.update", "id", conn.ID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "connection", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"connection": toConnectionInfo(*conn),
	}))
	emitAudit(m.eventBus, client, "connection.updated", "cli_connection", conn.ID.String())
}

func (m *ConnectionsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if !m.available(ctx, client, req.ID) {
		return
	}
	locale := store.LocaleFromContext(ctx)
	_, id, ok := parseConnectionParams(ctx, client, req, true)
	if !ok {
		return
	}
	conn := m.resolve(ctx, client, req.ID, id)
	if conn == nil {
		return
	}
	if !m.requireOwningScope(ctx, client, req.ID, conn) {
		return
	}
	// Pass the OWNING tenant; the store matches tenant_id exactly, so a tenant
	// can never delete another tenant's or a global row from here.
	if err := m.conns.Delete(ctx, conn.TenantID, conn.ID); err != nil {
		slog.Error("connections.delete", "id", conn.ID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "connection", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"deleted": true,
		"id":      conn.ID.String(),
	}))
	emitAudit(m.eventBus, client, "connection.deleted", "cli_connection", conn.ID.String())
}

// handleCredentialSet stores a BYOK secret for the ACTING USER (never
// tenant-shared). The secret is encrypted by the store and never echoed back.
func (m *ConnectionsMethods) handleCredentialSet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if !m.available(ctx, client, req.ID) {
		return
	}
	locale := store.LocaleFromContext(ctx)
	params, id, ok := parseConnectionParams(ctx, client, req, true)
	if !ok {
		return
	}
	userID := client.UserID()
	if userID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "user identity")))
		return
	}
	secret := strings.TrimSpace(params.Secret)
	if secret == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "secret")))
		return
	}
	credType := params.Type
	if credType == "" {
		credType = "api_key"
	}
	if credType != "api_key" && credType != "oauth" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "unsupported credential type (use api_key or oauth)")))
		return
	}
	conn := m.resolve(ctx, client, req.ID, id)
	if conn == nil {
		return
	}
	// Same provider → env-var table as the HTTP layer, so the sandbox injection
	// contract cannot drift between transports.
	inject := httpapi.CredentialInjectFor(conn.Provider, credType)
	if inject == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "this connection does not support a "+credType+" credential")))
		return
	}

	if err := m.conns.PutCredential(ctx, store.CLIConnectionCredential{
		ConnectionID: conn.ID,
		UserID:       userID, // per-user BYOK, never store.SharedCredentialUserID
		TenantID:     tenantScopeFromCtx(ctx),
		Type:         credType,
		Inject:       inject,
		Secret:       secret,
		UpdatedAt:    time.Now(),
	}); err != nil {
		slog.Error("connections.credential.set", "connection", conn.ID, "type", credType, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "credential", err.Error())))
		return
	}
	// Presence + shape only — never the secret or any prefix of it.
	slog.Info("connections.credential.saved", "connection", conn.ID, "provider", conn.Provider,
		"type", credType, "inject", inject, "secret_len", len(secret))
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"connectionId":     conn.ID.String(),
		"credentialType":   credType,
		"credentialStatus": "connected",
	}))
	emitAudit(m.eventBus, client, "connection.credential.set", "cli_connection", conn.ID.String())
}

// handleCredentialDelete removes the ACTING USER's secret for a connection; a
// tenant-shared fallback row (the empty user_id sentinel) is left untouched.
func (m *ConnectionsMethods) handleCredentialDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if !m.available(ctx, client, req.ID) {
		return
	}
	locale := store.LocaleFromContext(ctx)
	_, id, ok := parseConnectionParams(ctx, client, req, true)
	if !ok {
		return
	}
	userID := client.UserID()
	if userID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "user identity")))
		return
	}
	conn := m.resolve(ctx, client, req.ID, id)
	if conn == nil {
		return
	}
	if err := m.conns.DeleteCredential(ctx, conn.ID, userID); err != nil {
		slog.Error("connections.credential.delete", "connection", conn.ID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "credential", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"connectionId":     conn.ID.String(),
		"credentialStatus": "none",
	}))
	emitAudit(m.eventBus, client, "connection.credential.deleted", "cli_connection", conn.ID.String())
}

// available guards the write handlers when the store is not wired (older
// deployment / build without the table).
func (m *ConnectionsMethods) available(ctx context.Context, client *gateway.Client, reqID string) bool {
	if m.conns != nil {
		return true
	}
	locale := store.LocaleFromContext(ctx)
	client.SendResponse(protocol.NewErrorResponse(reqID, protocol.ErrInvalidRequest,
		i18n.T(locale, i18n.MsgInvalidRequest, "tenant CLI connections are not available on this deployment")))
	return false
}

// derefString returns the pointed-to string, or "" for nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// firstNonEmptyStr returns v when non-empty, else fallback.
func firstNonEmptyStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
