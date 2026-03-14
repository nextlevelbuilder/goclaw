package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *MCPHandler) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	var req store.MCPAccessRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if req.ServerID == uuid.Nil || req.Scope == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "server_id and scope")})
		return
	}
	if req.Scope != "agent" && req.Scope != "user" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "scope must be 'agent' or 'user'")})
		return
	}

	req.RequestedBy = store.UserIDFromContext(r.Context())
	req.Status = "pending"

	if err := h.store.CreateRequest(r.Context(), &req); err != nil {
		slog.Error("mcp.create_request", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "mcp_request.created", "mcp_request", req.ID.String())
	writeJSON(w, http.StatusCreated, req)
}

func (h *MCPHandler) handleListPendingRequests(w http.ResponseWriter, r *http.Request) {
	requests, err := h.store.ListPendingRequests(r.Context())
	if err != nil {
		slog.Error("mcp.list_pending_requests", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h *MCPHandler) handleReviewRequest(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	requestID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "request")})
		return
	}

	var req struct {
		Approved bool   `json:"approved"`
		Note     string `json:"note,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	reviewedBy := store.UserIDFromContext(r.Context())

	if err := h.store.ReviewRequest(r.Context(), requestID, req.Approved, reviewedBy, req.Note); err != nil {
		slog.Error("mcp.review_request", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if req.Approved {
		h.emitCacheInvalidate()
	}

	emitAudit(h.msgBus, r, "mcp_request.reviewed", "mcp_request", requestID.String())
	status := "rejected"
	if req.Approved {
		status = "approved"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}
