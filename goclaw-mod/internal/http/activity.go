package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ActivityHandler handles activity audit log endpoints.
type ActivityHandler struct {
	activity store.ActivityStore
}

// NewActivityHandler creates a handler for activity log endpoints.
func NewActivityHandler(activity store.ActivityStore) *ActivityHandler {
	return &ActivityHandler{activity: activity}
}

// RegisterRoutes registers activity routes on the given mux.
func (h *ActivityHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/activity/aggregate", h.authMiddleware(h.handleAggregate))
	mux.HandleFunc("GET /v1/activity", h.authMiddleware(h.handleList))
}

func (h *ActivityHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *ActivityHandler) handleAggregate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	auth := resolveAuth(r)
	groupBy := r.URL.Query().Get("group_by")
	if !validActivityGroupBy(groupBy) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid group_by"))
		return
	}
	if groupBy == "actor_id" && !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "actor_id aggregate"))
		return
	}

	opts := store.ActivityAggregateOpts{
		GroupBy: groupBy,
		ActivityListOpts: store.ActivityListOpts{
			ActorType:  r.URL.Query().Get("actor_type"),
			ActorID:    r.URL.Query().Get("actor_id"),
			Action:     r.URL.Query().Get("action"),
			EntityType: r.URL.Query().Get("entity_type"),
			EntityID:   r.URL.Query().Get("entity_id"),
			Limit:      parseActivityLimit(r.URL.Query().Get("limit")),
		},
	}
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		if callerID == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "user context required"))
			return
		}
		opts.ActorID = callerID
	}
	from, ok := parseOptionalRFC3339(r.URL.Query().Get("from"))
	if !ok {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "from must be RFC3339"))
		return
	}
	to, ok := parseOptionalRFC3339(r.URL.Query().Get("to"))
	if !ok {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "to must be RFC3339"))
		return
	}
	if from != nil && to != nil && !from.Before(*to) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "from must be before to"))
		return
	}
	opts.From = from
	opts.To = to

	buckets, total, err := h.activity.Aggregate(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	resp := map[string]any{
		"source":   "activity",
		"group_by": groupBy,
		"total":    total,
		"limit":    opts.Limit,
		"buckets":  buckets,
	}
	if from != nil {
		resp["from"] = from.UTC().Format(time.RFC3339)
	}
	if to != nil {
		resp["to"] = to.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

func validActivityGroupBy(groupBy string) bool {
	switch groupBy {
	case "action", "actor_type", "entity_type", "actor_id":
		return true
	default:
		return false
	}
}

func parseActivityLimit(raw string) int {
	if raw == "" {
		return 50
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

func parseOptionalRFC3339(raw string) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, false
	}
	return &t, true
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
	auth := resolveAuth(r)
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
