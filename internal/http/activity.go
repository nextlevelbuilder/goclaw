package http

import (
	"net/http"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ActivityHandler handles activity audit log endpoints.
type ActivityHandler struct {
	activity store.ActivityStore
	token    string
}

// NewActivityHandler creates a handler for activity log endpoints.
func NewActivityHandler(activity store.ActivityStore, token string) *ActivityHandler {
	return &ActivityHandler{activity: activity, token: token}
}

// RegisterRoutes registers activity routes on the given mux.
func (h *ActivityHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/activity", h.authMiddleware(h.handleList))
}

func (h *ActivityHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

func (h *ActivityHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.ActivityListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("actor_type"); v != "" {
		opts.ActorType = v
	}
	if v := r.URL.Query().Get("actor_id"); v != "" {
		opts.ActorID = v
	}

	// Non-admin callers may only see their own activity logs.
	auth := resolveAuth(r, h.token)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		opts.ActorID = callerID
	}
	if v := r.URL.Query().Get("action"); v != "" {
		opts.Action = v
	}
	if v := r.URL.Query().Get("entity_type"); v != "" {
		opts.EntityType = v
	}
	if v := r.URL.Query().Get("entity_id"); v != "" {
		opts.EntityID = v
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

	logs, err := h.activity.List(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	total, _ := h.activity.Count(r.Context(), opts)

	writeJSON(w, http.StatusOK, map[string]any{
		"logs":   logs,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}
