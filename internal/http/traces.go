package http

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TracesHandler handles LLM trace listing and detail endpoints.
type TracesHandler struct {
	tracing store.TracingStore
}

// NewTracesHandler creates a handler for trace management endpoints.
func NewTracesHandler(tracing store.TracingStore) *TracesHandler {
	return &TracesHandler{tracing: tracing}
}

// RegisterRoutes registers trace routes on the given mux.
func (h *TracesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/traces", h.authMiddleware(h.handleList))
	mux.HandleFunc("GET /v1/traces/{traceID}/export", h.authMiddleware(h.handleExport))
	mux.HandleFunc("GET /v1/traces/{traceID}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("GET /v1/costs/summary", h.authMiddleware(h.handleCostSummary))
}

func (h *TracesHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
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
	if v := r.URL.Query().Get("channel"); v != "" {
		opts.Channel = v
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

	// Non-admin callers may only see their own traces.
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		opts.UserID = callerID
	}

	traces, err := h.tracing.ListTraces(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	total, _ := h.tracing.CountTraces(r.Context(), opts)

	writeJSON(w, http.StatusOK, map[string]any{
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
		slog.Warn("traces.get_trace_failed", "trace_id", traceIDStr, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "trace", traceIDStr)})
		return
	}

	// Non-admin callers may only access their own traces.
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		if trace.UserID != callerID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "trace", traceIDStr)})
			return
		}
	}

	spans, err := h.tracing.GetTraceSpans(r.Context(), traceID)
	if err != nil {
		slog.Error("traces.get_spans_failed", "trace_id", traceIDStr, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
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

// traceExportEntry is a trace with its spans and recursive sub-traces.
type traceExportEntry struct {
	Trace     store.TraceData    `json:"trace"`
	Spans     []store.SpanData   `json:"spans"`
	SubTraces []traceExportEntry `json:"sub_traces,omitempty"`
}

func (h *TracesHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	traceID, err := uuid.Parse(r.PathValue("traceID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "trace")})
		return
	}

	// Verify ownership before export.
	rootTrace, err := h.tracing.GetTrace(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "trace", traceID.String())})
		return
	}
	authExport := resolveAuth(r)
	if !permissions.HasMinRole(authExport.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		if rootTrace.UserID != callerID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "trace", traceID.String())})
			return
		}
	}

	entry, err := h.collectTraceTree(r.Context(), traceID, 0)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "trace", traceID.String())})
		return
	}

	payload := struct {
		ExportedAt time.Time `json:"exported_at"`
		traceExportEntry
	}{
		ExportedAt:       time.Now().UTC(),
		traceExportEntry: *entry,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("trace-%s-%s.json.gz", traceID.String()[:8], time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	gz := gzip.NewWriter(w)
	defer gz.Close()
	gz.Write(data)
}

// collectTraceTree recursively collects a trace, its spans, and child traces.
func (h *TracesHandler) collectTraceTree(ctx context.Context, traceID uuid.UUID, depth int) (*traceExportEntry, error) {
	const maxDepth = 10
	trace, err := h.tracing.GetTrace(ctx, traceID)
	if err != nil {
		return nil, err
	}

	spans, _ := h.tracing.GetTraceSpans(ctx, traceID)

	entry := &traceExportEntry{Trace: *trace, Spans: spans}

	if depth >= maxDepth {
		return entry, nil
	}

	children, _ := h.tracing.ListChildTraces(ctx, traceID)
	for _, child := range children {
		sub, err := h.collectTraceTree(ctx, child.ID, depth+1)
		if err != nil {
			continue
		}
		entry.SubTraces = append(entry.SubTraces, *sub)
	}

	return entry, nil
}
