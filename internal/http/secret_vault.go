package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SecretVaultHandler manages vault entries for a tenant admin. It is write-only
// for secret values: no endpoint, response, log line or audit row carries one.
type SecretVaultHandler struct {
	store   store.SecretVaultStore
	tenants store.TenantStore
}

// NewSecretVaultHandler builds the handler. tenants authorises tenant admins.
func NewSecretVaultHandler(s store.SecretVaultStore, tenants store.TenantStore) *SecretVaultHandler {
	return &SecretVaultHandler{store: s, tenants: tenants}
}

// RegisterRoutes registers the vault routes.
func (h *SecretVaultHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/secret-vault", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/secret-vault", h.auth(h.handleCreate))
	mux.HandleFunc("PUT /v1/secret-vault/{id}", h.auth(h.handleUpdateMeta))
	mux.HandleFunc("PUT /v1/secret-vault/{id}/value", h.auth(h.handleReplaceValue))
	mux.HandleFunc("DELETE /v1/secret-vault/{id}", h.auth(h.handleDelete))
	mux.HandleFunc("GET /v1/secret-vault/{id}/grants", h.auth(h.handleListGrants))
	mux.HandleFunc("PUT /v1/secret-vault/{id}/grants", h.auth(h.handleSetGrants))
	mux.HandleFunc("GET /v1/secret-vault/{id}/audit", h.auth(h.handleAudit))
}

// auth requires an admin role, then membership as tenant admin: RoleAdmin alone
// is not a tenant check.
func (h *SecretVaultHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		if !requireTenantAdmin(w, r, h.tenants) {
			return
		}
		if store.TenantIDFromContext(r.Context()) == uuid.Nil {
			// A system owner without a tenant scope has no vault to manage.
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": store.ErrSecretVaultNoTenant.Error()})
			return
		}
		next(w, r)
	})
}

// secretVaultEntryView is the response shape of an entry: metadata only.
type secretVaultEntryView struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	AllowedDomains  []string  `json:"allowed_domains"`
	RequireApproval bool      `json:"require_approval"`
	Hints           string    `json:"hints"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       string    `json:"created_at"`
	UpdatedAt       string    `json:"updated_at"`
}

func toSecretVaultView(e store.SecretVaultEntry) secretVaultEntryView {
	domains := e.AllowedDomains
	if domains == nil {
		domains = []string{}
	}
	return secretVaultEntryView{
		ID: e.ID, Name: e.Name, Kind: e.Kind, AllowedDomains: domains,
		RequireApproval: e.RequireApproval, Hints: e.Hints, CreatedBy: e.CreatedBy,
		CreatedAt: e.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: e.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// writeVaultError maps store errors to a status and a message that never echoes input.
func writeVaultError(w http.ResponseWriter, r *http.Request, op string, err error) {
	locale := store.LocaleFromContext(r.Context())
	switch {
	case errors.Is(err, store.ErrSecretVaultNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "secret", "")})
	case errors.Is(err, store.ErrSecretVaultNameTaken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrSecretVaultInvalid), errors.Is(err, store.ErrSecretVaultAgentInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrSecretVaultNoKey):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		slog.Error("secret_vault."+op, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "secret vault " + op + " failed"})
	}
}

func (h *SecretVaultHandler) entryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "secret")})
		return uuid.Nil, false
	}
	return id, true
}

func decodeVaultBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(dst); err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return false
	}
	return true
}

// audit records an admin action; failure is logged but does not undo the change.
func (h *SecretVaultHandler) audit(r *http.Request, e *store.SecretVaultEntry, action string) {
	err := h.store.AppendAudit(r.Context(), &store.SecretVaultAuditEntry{
		EntryID: e.ID, EntryName: e.Name, UserID: store.UserIDFromContext(r.Context()),
		Action: action, Outcome: "ok",
	})
	if err != nil {
		slog.Warn("secret_vault.audit_failed", "action", action)
	}
}

func (h *SecretVaultHandler) handleList(w http.ResponseWriter, r *http.Request) {
	entries, err := h.store.List(r.Context())
	if err != nil {
		writeVaultError(w, r, "list", err)
		return
	}
	items := make([]secretVaultEntryView, 0, len(entries))
	for _, e := range entries {
		items = append(items, toSecretVaultView(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type secretVaultCreateRequest struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	AllowedDomains  []string `json:"allowed_domains"`
	RequireApproval *bool    `json:"require_approval,omitempty"`
	Hints           string   `json:"hints"`
	Username        string   `json:"username"`
	Secret          string   `json:"secret"`
}

func (h *SecretVaultHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req secretVaultCreateRequest
	if !decodeVaultBody(w, r, &req) {
		return
	}
	// login secrets default to asking before each use; api keys do not.
	approval := req.Kind == store.SecretKindLogin
	if req.RequireApproval != nil {
		approval = *req.RequireApproval
	}
	e := &store.SecretVaultEntry{
		Name: req.Name, Kind: req.Kind, AllowedDomains: req.AllowedDomains,
		RequireApproval: approval, Hints: req.Hints, CreatedBy: store.UserIDFromContext(r.Context()),
	}
	if err := h.store.Create(r.Context(), e, store.SecretVaultPayload{Username: req.Username, Secret: req.Secret}); err != nil {
		writeVaultError(w, r, "create", err)
		return
	}
	h.audit(r, e, store.SecretAuditCreated)
	created, err := h.store.Get(r.Context(), e.ID)
	if err != nil {
		writeVaultError(w, r, "create", err)
		return
	}
	writeJSON(w, http.StatusCreated, toSecretVaultView(*created))
}

type secretVaultMetaRequest struct {
	AllowedDomains  *[]string `json:"allowed_domains,omitempty"`
	RequireApproval *bool     `json:"require_approval,omitempty"`
	Hints           *string   `json:"hints,omitempty"`
}

func (h *SecretVaultHandler) handleUpdateMeta(w http.ResponseWriter, r *http.Request) {
	id, ok := h.entryID(w, r)
	if !ok {
		return
	}
	var req secretVaultMetaRequest
	if !decodeVaultBody(w, r, &req) {
		return
	}
	err := h.store.UpdateMeta(r.Context(), id, store.SecretVaultMetaUpdate{
		AllowedDomains: req.AllowedDomains, RequireApproval: req.RequireApproval, Hints: req.Hints,
	})
	if err != nil {
		writeVaultError(w, r, "update", err)
		return
	}
	e, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeVaultError(w, r, "update", err)
		return
	}
	h.audit(r, e, store.SecretAuditUpdated)
	writeJSON(w, http.StatusOK, toSecretVaultView(*e))
}

type secretVaultValueRequest struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

func (h *SecretVaultHandler) handleReplaceValue(w http.ResponseWriter, r *http.Request) {
	id, ok := h.entryID(w, r)
	if !ok {
		return
	}
	var req secretVaultValueRequest
	if !decodeVaultBody(w, r, &req) {
		return
	}
	if err := h.store.ReplaceValue(r.Context(), id, store.SecretVaultPayload{Username: req.Username, Secret: req.Secret}); err != nil {
		writeVaultError(w, r, "update", err)
		return
	}
	e, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeVaultError(w, r, "update", err)
		return
	}
	h.audit(r, e, store.SecretAuditUpdated)
	writeJSON(w, http.StatusOK, toSecretVaultView(*e))
}

func (h *SecretVaultHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.entryID(w, r)
	if !ok {
		return
	}
	e, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeVaultError(w, r, "delete", err)
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		writeVaultError(w, r, "delete", err)
		return
	}
	h.audit(r, e, store.SecretAuditDeleted)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *SecretVaultHandler) handleListGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := h.entryID(w, r)
	if !ok {
		return
	}
	if _, err := h.store.Get(r.Context(), id); err != nil {
		writeVaultError(w, r, "grants", err)
		return
	}
	ids, err := h.store.ListGrants(r.Context(), id)
	if err != nil {
		writeVaultError(w, r, "grants", err)
		return
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_ids": ids})
}

func (h *SecretVaultHandler) handleSetGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := h.entryID(w, r)
	if !ok {
		return
	}
	var req struct {
		AgentIDs []uuid.UUID `json:"agent_ids"`
	}
	if !decodeVaultBody(w, r, &req) {
		return
	}
	e, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeVaultError(w, r, "grants", err)
		return
	}
	if err := h.store.SetGrants(r.Context(), id, req.AgentIDs); err != nil {
		writeVaultError(w, r, "grants", err)
		return
	}
	h.audit(r, e, store.SecretAuditUpdated)
	if req.AgentIDs == nil {
		req.AgentIDs = []uuid.UUID{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_ids": req.AgentIDs})
}

func (h *SecretVaultHandler) handleAudit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.entryID(w, r)
	if !ok {
		return
	}
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	rows, err := h.store.ListAudit(r.Context(), id, limit)
	if err != nil {
		writeVaultError(w, r, "audit", err)
		return
	}
	if rows == nil {
		rows = []store.SecretVaultAuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}
