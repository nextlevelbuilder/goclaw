package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TracesHandler handles LLM trace listing and detail endpoints.
type TracesHandler struct {
	tracing store.TracingStore
	token   string
}

// NewTracesHandler creates a handler for trace management endpoints.
func NewTracesHandler(tracing store.TracingStore, token string) *TracesHandler {
	return &TracesHandler{tracing: tracing, token: token}
}

// RegisterRoutes registers trace routes on the given mux.
func (h *TracesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/traces", h.authMiddleware(h.handleList))
	mux.HandleFunc("GET /v1/traces/{traceID}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("GET /v1/costs/summary", h.authMiddleware(h.handleCostSummary))
}

func (h *TracesHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		locale := extractLocale(r)
		ctx := store.WithLocale(r.Context(), locale)
		r = r.WithContext(ctx)
		next(w, r)
	}
}

func (h *TracesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.TraceListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("agent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err == nil {
			opts.AgentID = &id
		}
	}
	if v := r.URL.Query().Get("user_id"); v != "" {
		opts.UserID = v
	}
	if v := r.URL.Query().Get("session_key"); v != "" {
		opts.SessionKey = v
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

	traces, err := h.tracing.ListTraces(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	total, _ := h.tracing.CountTraces(r.Context(), opts)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"traces": traces,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

func (h *TracesHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	traceIDStr := r.PathValue("traceID")
	traceID, err := uuid.Parse(traceIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "trace")})
		return
	}

	trace, err := h.tracing.GetTrace(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "trace", traceIDStr)})
		return
	}

	spans, err := h.tracing.GetTraceSpans(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"trace": trace,
		"spans": spans,
	})
}

func (h *TracesHandler) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	opts := store.CostSummaryOpts{}

	if v := r.URL.Query().Get("agent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err == nil {
			opts.AgentID = &id
		}
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.From = &t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.To = &t
		}
	}

	rows, err := h.tracing.GetCostSummary(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}
