package http

import (
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	runtimelogs "github.com/nextlevelbuilder/goclaw/internal/logs"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type RuntimeLogSnapshotter interface {
	AggregateRuntimeLogs(opts runtimelogs.RuntimeAggregateOpts) runtimelogs.RuntimeAggregateResult
}

type RuntimeLogsHandler struct {
	snapshotter RuntimeLogSnapshotter
}

func NewRuntimeLogsHandler(snapshotter RuntimeLogSnapshotter) *RuntimeLogsHandler {
	return &RuntimeLogsHandler{snapshotter: snapshotter}
}

func (h *RuntimeLogsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/logs/runtime/aggregate", requireAuth(permissions.RoleAdmin, h.handleAggregate))
}

func (h *RuntimeLogsHandler) handleAggregate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.snapshotter == nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInvalidRequest, "runtime logs unavailable"))
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "level"
	}
	if groupBy != "level" && groupBy != "source" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid group_by"))
		return
	}
	fromMS := int64(0)
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "from must be RFC3339"))
			return
		}
		fromMS = from.UnixMilli()
	}
	result := h.snapshotter.AggregateRuntimeLogs(runtimelogs.RuntimeAggregateOpts{
		GroupBy: groupBy,
		Level:   r.URL.Query().Get("level"),
		Source:  r.URL.Query().Get("source"),
		FromMS:  fromMS,
	})
	writeJSON(w, http.StatusOK, result)
}
