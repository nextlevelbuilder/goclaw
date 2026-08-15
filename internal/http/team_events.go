package http

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TeamEventsHandler handles team event history HTTP endpoints.
type TeamEventsHandler struct {
	teamStore store.TeamStore
}

func NewTeamEventsHandler(teamStore store.TeamStore) *TeamEventsHandler {
	return &TeamEventsHandler{teamStore: teamStore}
}

func (h *TeamEventsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/teams/{id}/events", h.authMiddleware(h.handleList))
}

func (h *TeamEventsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *TeamEventsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if h.teamStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTeamsNotConfigured)})
		return
	}

	teamID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team id"})
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	events, err := h.teamStore.ListTeamEvents(r.Context(), teamID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if events == nil {
		events = []store.TeamTaskEventData{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}
