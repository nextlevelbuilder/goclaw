package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SecureCLIGrantHandler handles CRUD for per-agent secure CLI grants.
type SecureCLIGrantHandler struct {
	grants store.SecureCLIAgentGrantStore
	msgBus *bus.MessageBus
}

func NewSecureCLIGrantHandler(gs store.SecureCLIAgentGrantStore, msgBus *bus.MessageBus) *SecureCLIGrantHandler {
	return &SecureCLIGrantHandler{grants: gs, msgBus: msgBus}
}

// RegisterRoutes registers agent grant routes nested under cli-credentials.
func (h *SecureCLIGrantHandler) RegisterRoutes(mux *http.ServeMux) {
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAuth(permissions.RoleAdmin, next)
	}
	mux.HandleFunc("GET /v1/cli-credentials/{id}/agent-grants", auth(h.handleList))
	mux.HandleFunc("POST /v1/cli-credentials/{id}/agent-grants", auth(h.handleCreate))
	mux.HandleFunc("GET /v1/cli-credentials/{id}/agent-grants/{grantId}", auth(h.handleGet))
	mux.HandleFunc("PUT /v1/cli-credentials/{id}/agent-grants/{grantId}", auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/cli-credentials/{id}/agent-grants/{grantId}", auth(h.handleDelete))
}

func (h *SecureCLIGrantHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}
	grants, err := h.grants.ListByBinary(r.Context(), binaryID)
	if err != nil {
		slog.Error("secure_cli_grants.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "grants")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

type grantCreateRequest struct {
	AgentID        uuid.UUID        `json:"agent_id"`
	DenyArgs       *json.RawMessage `json:"deny_args,omitempty"`
	DenyVerbose    *json.RawMessage `json:"deny_verbose,omitempty"`
	TimeoutSeconds *int             `json:"timeout_seconds,omitempty"`
	Tips           *string          `json:"tips,omitempty"`
	Enabled        *bool            `json:"enabled,omitempty"`
}

func (h *SecureCLIGrantHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}

	var req grantCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if req.AgentID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "agent_id")})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	g := &store.SecureCLIAgentGrant{
		BinaryID:       binaryID,
		AgentID:        req.AgentID,
		DenyArgs:       req.DenyArgs,
		DenyVerbose:    req.DenyVerbose,
		TimeoutSeconds: req.TimeoutSeconds,
		Tips:           req.Tips,
		Enabled:        enabled,
	}
	if err := h.grants.Create(r.Context(), g); err != nil {
		slog.Error("secure_cli_grants.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate(binaryID.String())
	writeJSON(w, http.StatusCreated, g)
}

func (h *SecureCLIGrantHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	grantID, err := uuid.Parse(r.PathValue("grantId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "grant")})
		return
	}
	g, err := h.grants.Get(r.Context(), grantID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "grant", grantID.String())})
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (h *SecureCLIGrantHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	grantID, err := uuid.Parse(r.PathValue("grantId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "grant")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	updates["updated_at"] = time.Now()
	if err := h.grants.Update(r.Context(), grantID, updates); err != nil {
		slog.Error("secure_cli_grants.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	binaryID := r.PathValue("id")
	h.emitCacheInvalidate(binaryID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SecureCLIGrantHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	grantID, err := uuid.Parse(r.PathValue("grantId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "grant")})
		return
	}
	if err := h.grants.Delete(r.Context(), grantID); err != nil {
		slog.Error("secure_cli_grants.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	binaryID := r.PathValue("id")
	h.emitCacheInvalidate(binaryID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SecureCLIGrantHandler) emitCacheInvalidate(key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: map[string]any{"scope": "secure_cli", "key": key},
	})
}
