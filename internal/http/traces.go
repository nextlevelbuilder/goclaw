package http

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TracesHandler handles LLM trace listing and detail endpoints.
type TracesHandler struct {
	tracing     store.TracingStore
	runTimeline store.RunTimelineStore
	runs        store.RunsStore

	// resumer resumes a checkpointed run (wired to agent.Loop.ResumeRun by cmd).
	// Nil = POST /v1/runs/{runID}/resume reports unavailable.
	resumer func(ctx context.Context, runID string) (*agent.RunResult, error)
}

// NewTracesHandler creates a handler for trace management endpoints.
func NewTracesHandler(tracing store.TracingStore, timelines ...store.RunTimelineStore) *TracesHandler {
	h := &TracesHandler{tracing: tracing}
	if len(timelines) > 0 {
		h.runTimeline = timelines[0]
	}
	return h
}

// SetRunsStore attaches the durable run-record store, enabling the
// GET /v1/runs/{runID} and GET /v1/runs/{runID}/events endpoints. Nil-safe:
// those endpoints report an unavailable error when the store is not wired.
func (h *TracesHandler) SetRunsStore(runs store.RunsStore) { h.runs = runs }

// SetResumer wires the resume entrypoint for POST /v1/runs/{runID}/resume.
// Nil-safe: the endpoint reports unavailable until the loop is attached.
func (h *TracesHandler) SetResumer(resume func(ctx context.Context, runID string) (*agent.RunResult, error)) {
	h.resumer = resume
}

// RegisterRoutes registers trace routes on the given mux.
func (h *TracesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/traces", h.authMiddleware(h.handleList))
	mux.HandleFunc("GET /v1/traces/follow", h.authMiddleware(h.handleFollow))
	mux.HandleFunc("GET /v1/traces/{traceID}/export", h.authMiddleware(h.handleExport))
	mux.HandleFunc("GET /v1/traces/{traceID}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("GET /v1/runs/{runID}/timeline", h.authMiddleware(h.handleRunTimeline))
	// Durable run records (agent_runs state machine). Go's ServeMux prefers the
	// more specific /{runID}/timeline and /{runID}/events patterns for those
	// paths, so GET /v1/runs/{runID} matches run records only.
	mux.HandleFunc("GET /v1/runs/{runID}", h.authMiddleware(h.handleRunGet))
	mux.HandleFunc("GET /v1/runs/{runID}/events", h.authMiddleware(h.handleRunEvents))
	mux.HandleFunc("POST /v1/runs/{runID}/resume", h.authMiddleware(h.handleRunResume))
	mux.HandleFunc("GET /v1/costs/summary", h.authMiddleware(h.handleCostSummary))
}

func (h *TracesHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *TracesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	query := r.URL.Query()
	opts := store.TraceListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := query.Get("agent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err == nil {
			opts.AgentID = &id
		}
	}
	if v := query.Get("user_id"); v != "" {
		opts.UserID = v
	}
	if v := query.Get("session_key"); v != "" {
		opts.SessionKey = v
	}
	if v := query.Get("status"); v != "" {
		opts.Status = v
	}
	if v := query.Get("channel"); v != "" {
		opts.Channel = v
	}
	if v := strings.TrimSpace(query.Get("q")); v != "" {
		opts.Query = v
	}
	if v := strings.TrimSpace(query.Get("agent")); v != "" {
		opts.AgentQuery = v
	}
	if v := strings.TrimSpace(query.Get("channel_query")); v != "" {
		opts.ChannelQuery = v
	}
	if v := strings.TrimSpace(query.Get("tool_name")); v != "" {
		opts.ToolName = v
	}
	if parsed, ok, err := parseRFC3339Query(query.Get("from")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "from must be RFC3339")})
		return
	} else if ok {
		opts.From = &parsed
	}
	if parsed, ok, err := parseRFC3339Query(query.Get("to")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "to must be RFC3339")})
		return
	} else if ok {
		opts.To = &parsed
	}
	if n, ok := parseNonNegativeIntQuery(query.Get("min_input_tokens")); ok {
		opts.MinInputTokens = &n
	}
	if n, ok := parseNonNegativeIntQuery(query.Get("max_input_tokens")); ok {
		opts.MaxInputTokens = &n
	}
	if n, ok := parseNonNegativeIntQuery(query.Get("min_output_tokens")); ok {
		opts.MinOutputTokens = &n
	}
	if n, ok := parseNonNegativeIntQuery(query.Get("max_output_tokens")); ok {
		opts.MaxOutputTokens = &n
	}
	if n, ok := parseNonNegativeIntQuery(query.Get("min_tool_calls")); ok {
		opts.MinToolCalls = &n
	}
	if n, ok := parseNonNegativeIntQuery(query.Get("max_tool_calls")); ok {
		opts.MaxToolCalls = &n
	}
	if v := query.Get("has_tool_calls"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "has_tool_calls must be boolean")})
			return
		}
		opts.HasToolCalls = &parsed
	}
	if v := query.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			opts.Limit = n
		}
	}
	if v := query.Get("offset"); v != "" {
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

func parseRFC3339Query(v string) (time.Time, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed, true, nil
}

func parseNonNegativeIntQuery(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (h *TracesHandler) handleFollow(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	query := r.URL.Query()
	opts := store.TraceListOpts{
		Limit: 50,
	}

	if v := query.Get("agent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
			return
		}
		opts.AgentID = &id
	}
	if v := query.Get("session_key"); v != "" {
		opts.SessionKey = v
	}
	if opts.SessionKey == "" && opts.AgentID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "session_key or agent_id")})
		return
	}
	if v := query.Get("status"); v != "" {
		opts.Status = v
	}
	if v := query.Get("channel"); v != "" {
		opts.Channel = v
	}
	if v := query.Get("since"); v != "" {
		since, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "since must be RFC3339")})
			return
		}
		opts.ChangedAfter = &since
	}
	if v := query.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			opts.Limit = n
		}
	}
	includeSpans := false
	if v := query.Get("include_spans"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "include_spans must be boolean")})
			return
		}
		includeSpans = parsed
	}

	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		opts.UserID = store.UserIDFromContext(r.Context())
	} else if v := query.Get("user_id"); v != "" {
		opts.UserID = v
	}

	serverTime := time.Now().UTC()
	traces, err := h.tracing.ListTraces(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	spansByTraceID := map[string][]store.SpanData{}
	if includeSpans {
		for _, trace := range traces {
			spans, err := h.tracing.GetTraceSpans(r.Context(), trace.ID)
			if err != nil {
				slog.Error("traces.follow_get_spans_failed", "trace_id", trace.ID, "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			spansByTraceID[trace.ID.String()] = spans
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"traces":            traces,
		"spans_by_trace_id": spansByTraceID,
		"server_time":       serverTime,
		"next_since":        serverTime,
		"limit":             opts.Limit,
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

func (h *TracesHandler) handleRunTimeline(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.runTimeline == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgRunTimelineUnavailable)})
		return
	}
	runID := r.PathValue("runID")
	if strings.TrimSpace(runID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "run_id")})
		return
	}
	opts := store.RunTimelineListOpts{
		RunID:      runID,
		SessionKey: r.URL.Query().Get("session_key"),
		Limit:      200,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}
	items, err := h.runTimeline.ListRunTimelineItems(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		items = filterTimelineItemsByUser(items, callerID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":      runID,
		"session_key": opts.SessionKey,
		"items":       items,
		"limit":       opts.Limit,
		"offset":      opts.Offset,
	})
}

// handleRunGet serves GET /v1/runs/{runID}: one durable run record.
func (h *TracesHandler) handleRunGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.runs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgRunsUnavailable)})
		return
	}
	runID := r.PathValue("runID")
	if strings.TrimSpace(runID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "run_id")})
		return
	}
	run, err := h.runs.GetRun(r.Context(), runID)
	if err != nil {
		slog.Warn("runs.get_failed", "run_id", runID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "run", runID)})
		return
	}
	// Non-admin callers may only read their own run records.
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		if run.UserID != callerID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "run", runID)})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

// handleRunEvents serves GET /v1/runs/{runID}/events?after=N: cursor replay of
// run timeline items after a seq cursor (for durable-run resync).
func (h *TracesHandler) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.runTimeline == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgRunTimelineUnavailable)})
		return
	}
	runID := r.PathValue("runID")
	if strings.TrimSpace(runID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "run_id")})
		return
	}
	opts := store.RunTimelineListOpts{RunID: runID, Limit: 200}
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "after must be a non-negative integer")})
			return
		}
		opts.AfterSeq = n
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			opts.Limit = n
		}
	}
	items, err := h.runTimeline.ListRunTimelineItems(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		callerID := store.UserIDFromContext(r.Context())
		items = filterTimelineItemsByUser(items, callerID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":    runID,
		"after":     opts.AfterSeq,
		"items":     items,
		"limit":     opts.Limit,
		"next_after": nextTimelineAfter(items, opts.AfterSeq),
	})
}

// nextTimelineAfter computes the cursor for the next page: the seq of the last
// item returned (or the input cursor when no items were returned).
func nextTimelineAfter(items []store.RunTimelineItem, after int) int {
	if len(items) == 0 {
		return after
	}
	last := items[len(items)-1].Seq
	if last > after {
		return last
	}
	return after
}

func filterTimelineItemsByUser(items []store.RunTimelineItem, userID string) []store.RunTimelineItem {
	if userID == "" {
		return nil
	}
	filtered := items[:0]
	for _, item := range items {
		if item.UserID == userID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// handleRunResume serves POST /v1/runs/{runID}/resume: resumes a checkpointed
// durable run synchronously. Body is unused (resume replays the recorded
// checkpoint). Returns the fresh run record + final result.
func (h *TracesHandler) handleRunResume(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.resumer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgRunsUnavailable)})
		return
	}
	runID := r.PathValue("runID")
	if strings.TrimSpace(runID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "run_id")})
		return
	}
	// Non-admin callers may only resume their own runs (parity with handleRunGet).
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		if h.runs == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgRunsUnavailable)})
			return
		}
		run, err := h.runs.GetRun(r.Context(), runID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "run", runID)})
			return
		}
		callerID := store.UserIDFromContext(r.Context())
		if run.UserID != callerID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "run", runID)})
			return
		}
	}
	result, err := h.resumer(r.Context(), runID)
	if err != nil {
		slog.Warn("runs.resume_failed", "run_id", runID, "error", err)
		if errors.Is(err, agent.ErrRunResumeNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "run", runID)})
			return
		}
		if errors.Is(err, agent.ErrRunResumeUnavailable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgRunsUnavailable)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "resume run")})
		return
	}
	var run *store.AgentRun
	if h.runs != nil {
		if run, err = h.runs.GetRun(r.Context(), runID); err != nil {
			slog.Warn("runs.resume_refetch_failed", "run_id", runID, "error", err)
			run = nil
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "result": result})
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
