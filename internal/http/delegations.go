package http

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DelegationsHandler handles delegation history HTTP endpoints.
type DelegationsHandler struct {
	teamStore store.TeamStore
	token     string
}

func NewDelegationsHandler(teamStore store.TeamStore, token string) *DelegationsHandler {
	return &DelegationsHandler{teamStore: teamStore, token: token}
}

func (h *DelegationsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/delegations", h.authMiddleware(h.handleList))
	mux.HandleFunc("GET /v1/delegations/{id}", h.authMiddleware(h.handleGet))
}

func (h *DelegationsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

func (h *DelegationsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.DelegationHistoryListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("source_agent_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			opts.SourceAgentID = &id
		}
	}
	if v := r.URL.Query().Get("target_agent_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			opts.TargetAgentID = &id
		}
	}
	if v := r.URL.Query().Get("team_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			opts.TeamID = &id
		}
	}
	if v := r.URL.Query().Get("user_id"); v != "" {
		opts.UserID = v
	}
	if v := r.URL.Query().Get("status"); v != "" {
		opts.Status = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	records, total, err := h.teamStore.ListDelegationHistory(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": records,
		"total":   total,
	})
}

func (h *DelegationsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "delegation")})
		return
	}

	record, err := h.teamStore.GetDelegationHistory(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, record)
}
