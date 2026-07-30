package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Tenant-level CLI connections (migration 000082).
//
// A connection is owned by a TENANT, not by one agent — every agent in the
// tenant may delegate to any enabled connection. These endpoints are the write
// surface for that catalogue; the older per-agent endpoints in agents.go remain
// in place this release (expand/contract).
//
// Secrets: the connection row itself never holds one. BYOK secrets live in
// cli_connection_credentials, are scoped to the ACTING USER, are encrypted at
// rest by the store, and are NEVER returned by any handler here — only their
// presence + type.

// connectionView is the wire shape of a connection. Explicit (rather than
// marshalling store.CLIConnection) so a future secret-bearing field on the
// domain struct cannot leak into a response by accident.
type connectionView struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id,omitempty"`
	Global    bool            `json:"global"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Provider  string          `json:"provider"`
	Mode      string          `json:"mode"`
	Endpoint  string          `json:"endpoint,omitempty"`
	Enabled   bool            `json:"enabled"`
	Config    json.RawMessage `json:"config,omitempty"`
	CreatedBy string          `json:"created_by,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	// Resolved server-side from the adapter spec (defaults + this connection's
	// config). Clients must not re-derive these from the provider name —
	// interactive_args/manual_approve_args are config-overridable, so a
	// provider-name check hides capabilities a connection really has.
	SupportsInteractive    bool `json:"supports_interactive"`
	SupportsManualApproval bool `json:"supports_manual_approval"`
	SupportsOAuthLogin     bool `json:"supports_oauth_login"`
}

func toConnectionView(c store.CLIConnection) connectionView {
	spec, specErr := cliagent.Resolve(c.Provider, c.Config)
	v := connectionView{
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
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
	if specErr == nil {
		v.SupportsInteractive = spec.SupportsInteractive()
		v.SupportsManualApproval = spec.SupportsManualApproval()
	}
	v.SupportsOAuthLogin = injectForConnectionCfg(c.Provider, "oauth", c.Config) != ""
	if c.TenantID != nil {
		v.TenantID = c.TenantID.String()
	}
	return v
}

// connectionRequest is the create/update body. Pointer fields let PUT patch a
// single attribute (e.g. {"enabled": false}) without clobbering the rest.
type connectionRequest struct {
	Name     *string          `json:"name"`
	Kind     *string          `json:"kind"`
	Provider *string          `json:"provider"`
	Mode     *string          `json:"mode"`
	Endpoint *string          `json:"endpoint"`
	Enabled  *bool            `json:"enabled"`
	Config   *json.RawMessage `json:"config"`
}

// setCLIConnectionCredentialRequest is the body for
// PUT /v1/connections/{id}/credential.
type setCLIConnectionCredentialRequest struct {
	// Type is "api_key" or "oauth". "oauth" is normally written by the
	// "Log in with Claude" flow below, but a caller holding a token already may
	// save it directly.
	Type   string `json:"type"`
	Secret string `json:"secret"`
}

// tenantScope returns the caller's tenant as the *uuid.UUID the store expects.
// nil means "platform scope": reads then see ONLY global rows and writes target
// global rows (gated by requireMasterScope).
func tenantScope(ctx context.Context) *uuid.UUID {
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		return &tid
	}
	return nil
}

// connectionsAvailable reports whether the tenant-level connection store is
// wired; when it is not (older deployment / lite build without the table) the
// endpoints answer 501 rather than panicking.
func (h *AgentsHandler) connectionsAvailable(w http.ResponseWriter) bool {
	if h.cliConns == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrInternal, "tenant CLI connections are not available on this deployment")
		return false
	}
	return true
}

// resolveConnection loads a connection visible to the caller's tenant (its own
// row or a global one). On failure it writes the response and returns nil.
func (h *AgentsHandler) resolveConnection(w http.ResponseWriter, r *http.Request) *store.CLIConnection {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "connection"))
		return nil
	}
	conn, err := h.cliConns.GetByID(ctx, tenantScope(ctx), id)
	if err != nil {
		slog.Error("connections.get", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "connection"))
		return nil
	}
	if conn == nil {
		// Not visible to this tenant is indistinguishable from absent — never
		// confirm that another tenant's row exists.
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "connection", id.String()))
		return nil
	}
	return conn
}

// handleListConnections returns the caller's tenant connections plus the global
// ones. Never includes secrets.
func (h *AgentsHandler) handleListConnections(w http.ResponseWriter, r *http.Request) {
	if !h.connectionsAvailable(w) {
		return
	}
	ctx := r.Context()
	conns, err := h.cliConns.ListForTenant(ctx, tenantScope(ctx), false)
	if err != nil {
		slog.Error("connections.list", "tenant_id", store.TenantIDFromContext(ctx), "error", err)
		locale := store.LocaleFromContext(ctx)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "connections"))
		return
	}
	out := make([]connectionView, 0, len(conns))
	for _, c := range conns {
		out = append(out, toConnectionView(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// handleCreateConnection creates a tenant connection. A caller with no tenant in
// context creates a GLOBAL connection, which requires master scope.
func (h *AgentsHandler) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	if !h.connectionsAvailable(w) {
		return
	}
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	var req connectionRequest
	if !bindJSON(w, r, locale, &req) {
		return
	}
	name := strings.TrimSpace(derefStr(req.Name))
	if name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}
	provider := strings.TrimSpace(strings.ToLower(derefStr(req.Provider)))
	if provider == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "provider"))
		return
	}

	tenantID := tenantScope(ctx)
	// tenant_id NULL is a platform-wide row: same rule as builtin_tools / disk
	// config — RoleAdmin inside one tenant is not enough.
	if tenantID == nil && !requireMasterScope(w, r) {
		return
	}

	conn := &store.CLIConnection{
		TenantID:  tenantID,
		Name:      name,
		Kind:      firstNonEmpty(strings.TrimSpace(derefStr(req.Kind)), "external_cli"),
		Provider:  provider,
		Mode:      firstNonEmpty(strings.TrimSpace(derefStr(req.Mode)), "delegate"),
		Endpoint:  strings.TrimSpace(derefStr(req.Endpoint)),
		Enabled:   req.Enabled == nil || *req.Enabled, // default enabled
		CreatedBy: store.UserIDFromContext(ctx),
	}
	if req.Config != nil {
		conn.Config = *req.Config
	}
	if err := h.cliConns.Upsert(ctx, conn); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, protocol.ErrAlreadyExists, i18n.T(locale, i18n.MsgAlreadyExists, "connection", name))
			return
		}
		slog.Error("connections.create", "name", name, "provider", provider, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "connection", "internal error"))
		return
	}
	emitAudit(h.msgBus, r, "connection.created", "cli_connection", conn.ID.String())
	writeJSON(w, http.StatusCreated, toConnectionView(*conn))
}

// handleUpdateConnection patches a connection. Only the owning scope may mutate:
// a tenant admin can edit their tenant's rows, and a global row needs master
// scope (the store's UPDATE additionally matches tenant_id exactly).
func (h *AgentsHandler) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	if !h.connectionsAvailable(w) {
		return
	}
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	conn := h.resolveConnection(w, r)
	if conn == nil {
		return
	}
	if conn.TenantID == nil && !requireMasterScope(w, r) {
		return
	}

	var req connectionRequest
	if !bindJSON(w, r, locale, &req) {
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
			return
		}
		conn.Name = name
	}
	if req.Kind != nil && strings.TrimSpace(*req.Kind) != "" {
		conn.Kind = strings.TrimSpace(*req.Kind)
	}
	if req.Provider != nil && strings.TrimSpace(*req.Provider) != "" {
		conn.Provider = strings.ToLower(strings.TrimSpace(*req.Provider))
	}
	if req.Mode != nil && strings.TrimSpace(*req.Mode) != "" {
		conn.Mode = strings.TrimSpace(*req.Mode)
	}
	if req.Endpoint != nil {
		conn.Endpoint = strings.TrimSpace(*req.Endpoint)
	}
	if req.Enabled != nil {
		conn.Enabled = *req.Enabled
	}
	if req.Config != nil {
		conn.Config = *req.Config
	}

	if err := h.cliConns.Upsert(ctx, conn); err != nil {
		slog.Error("connections.update", "id", conn.ID, "tenant_id", store.TenantIDFromContext(ctx), "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "connection", "internal error"))
		return
	}
	emitAudit(h.msgBus, r, "connection.updated", "cli_connection", conn.ID.String())
	writeJSON(w, http.StatusOK, toConnectionView(*conn))
}

// handleDeleteConnection removes a connection (credentials cascade in the DB).
func (h *AgentsHandler) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	if !h.connectionsAvailable(w) {
		return
	}
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	conn := h.resolveConnection(w, r)
	if conn == nil {
		return
	}
	if conn.TenantID == nil && !requireMasterScope(w, r) {
		return
	}
	// Pass the OWNING tenant (which, for a non-global row, is the caller's own —
	// GetByID already proved visibility). The store matches tenant_id exactly, so
	// a tenant can never delete another tenant's or a global row through here.
	if err := h.cliConns.Delete(ctx, conn.TenantID, conn.ID); err != nil {
		slog.Error("connections.delete", "id", conn.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "connection", "internal error"))
		return
	}
	emitAudit(h.msgBus, r, "connection.deleted", "cli_connection", conn.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": conn.ID.String()})
}

// handleSetCLIConnectionCredential stores a BYOK secret for the ACTING USER —
// not tenant-shared — so one user's Anthropic key/token is never handed to
// their team-mates. The secret is encrypted by the store and never echoed back.
func (h *AgentsHandler) handleSetCLIConnectionCredential(w http.ResponseWriter, r *http.Request) {
	if !h.connectionsAvailable(w) {
		return
	}
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDHeader))
		return
	}
	conn := h.resolveConnection(w, r)
	if conn == nil {
		return
	}

	var req setCLIConnectionCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "invalid request body")
		return
	}
	secret := strings.TrimSpace(req.Secret)
	if secret == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "secret is required")
		return
	}
	credType := req.Type
	if credType == "" {
		credType = "api_key"
	}
	if credType != "api_key" && credType != "oauth" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "unsupported credential type (use api_key or oauth)")
		return
	}
	// Same provider → env-var mapping the per-agent path uses, so the sandbox
	// injection contract stays identical for both catalogues.
	inject := injectForConnectionCfg(conn.Provider, credType, conn.Config)
	if inject == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "this connection does not support a "+credType+" credential")
		return
	}

	if err := h.cliConns.PutCredential(ctx, store.CLIConnectionCredential{
		ConnectionID: conn.ID,
		UserID:       userID, // per-user BYOK, never store.SharedCredentialUserID
		// The credential belongs to the caller's tenant even when the connection
		// itself is global, so tenant deletion cleans it up.
		TenantID: tenantScope(ctx),
		Type:     credType,
		Inject:   inject,
		Secret:   secret,
	}); err != nil {
		slog.Error("connections.credential.set", "connection", conn.ID, "type", credType, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to store credential")
		return
	}
	// Presence + shape only — never the secret or any prefix of it.
	slog.Info("connections.credential.saved", "connection", conn.ID, "provider", conn.Provider,
		"type", credType, "inject", inject, "secret_len", len(secret))
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     conn.ID.String(),
		"credential_type":   credType,
		"credential_status": "connected",
	})
}

// handleDeleteCLIConnectionCredential removes the ACTING USER's secret for a
// connection. A tenant-shared fallback row (the empty user_id sentinel) is left
// untouched.
func (h *AgentsHandler) handleDeleteCLIConnectionCredential(w http.ResponseWriter, r *http.Request) {
	if !h.connectionsAvailable(w) {
		return
	}
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDHeader))
		return
	}
	conn := h.resolveConnection(w, r)
	if conn == nil {
		return
	}
	if err := h.cliConns.DeleteCredential(ctx, conn.ID, userID); err != nil {
		slog.Error("connections.credential.delete", "connection", conn.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to delete credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     conn.ID.String(),
		"credential_status": "none",
	})
}

// handleListCLIConnectionCredentials reports WHICH visible connections resolve to
// a credential for the acting user — booleans and the credential type only, so
// the UI can render "Connected ✓" after a reload. Never returns a secret.
func (h *AgentsHandler) handleListCLIConnectionCredentials(w http.ResponseWriter, r *http.Request) {
	out := map[string]map[string]any{}
	if h.cliConns == nil {
		writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
		return
	}
	ctx := r.Context()
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		locale := store.LocaleFromContext(ctx)
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDHeader))
		return
	}
	conns, err := h.cliConns.ListForTenant(ctx, tenantScope(ctx), false)
	if err != nil {
		slog.Error("connections.credentials.list", "error", err)
		locale := store.LocaleFromContext(ctx)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "credentials"))
		return
	}
	for _, c := range conns {
		cred, err := h.cliConns.GetCredential(ctx, c.ID, userID)
		if err != nil {
			slog.Warn("connections.credentials.list_lookup_failed", "connection", c.ID, "error", err)
			continue
		}
		if cred == nil {
			continue
		}
		out[c.ID.String()] = map[string]any{
			"status": "connected",
			"type":   cred.Type,
			// true when this user has no row of their own and resolved to the
			// tenant-shared fallback.
			"shared": cred.UserID == store.SharedCredentialUserID,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// CredentialInjectFor maps (provider, credType) to the sandbox injection
// descriptor ("env:VAR" / "file:PATH"), or "" when the combination is
// unsupported. Exported so the WS method layer derives the SAME descriptor as
// these handlers instead of keeping a second copy of the table.
func CredentialInjectFor(provider, credType string, cfg json.RawMessage) string {
	return injectForConnectionCfg(provider, credType, cfg)
}

// derefStr returns the pointed-to string, or "" for nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
