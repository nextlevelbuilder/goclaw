package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type mockTracingStore struct {
	listOpts []store.TraceListOpts
	traces   []store.TraceData
	spans    map[uuid.UUID][]store.SpanData
}

func (m *mockTracingStore) CreateTrace(context.Context, *store.TraceData) error { return nil }
func (m *mockTracingStore) UpdateTrace(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (m *mockTracingStore) GetTrace(context.Context, uuid.UUID) (*store.TraceData, error) {
	return nil, errors.New("not found")
}
func (m *mockTracingStore) ListTraces(_ context.Context, opts store.TraceListOpts) ([]store.TraceData, error) {
	m.listOpts = append(m.listOpts, opts)
	return m.traces, nil
}
func (m *mockTracingStore) CountTraces(context.Context, store.TraceListOpts) (int, error) {
	return len(m.traces), nil
}
func (m *mockTracingStore) CreateSpan(context.Context, *store.SpanData) error { return nil }
func (m *mockTracingStore) UpdateSpan(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (m *mockTracingStore) GetTraceSpans(_ context.Context, traceID uuid.UUID) ([]store.SpanData, error) {
	return m.spans[traceID], nil
}
func (m *mockTracingStore) ListChildTraces(context.Context, uuid.UUID) ([]store.TraceData, error) {
	return nil, nil
}
func (m *mockTracingStore) BatchCreateSpans(context.Context, []store.SpanData) error {
	return nil
}
func (m *mockTracingStore) BatchUpdateTraceAggregates(context.Context, uuid.UUID) error {
	return nil
}
func (m *mockTracingStore) GetMonthlyAgentCost(context.Context, uuid.UUID, int, time.Month) (float64, error) {
	return 0, nil
}
func (m *mockTracingStore) GetCostSummary(context.Context, store.CostSummaryOpts) ([]store.CostSummaryRow, error) {
	return nil, nil
}
func (m *mockTracingStore) DeleteTracesOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (m *mockTracingStore) RecoverStaleRunningTraces(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (m *mockTracingStore) ListCodexPoolSpans(context.Context, uuid.UUID, uuid.UUID, []string, int) ([]store.CodexPoolSpan, error) {
	return nil, nil
}
func (m *mockTracingStore) ListCodexPoolSpansByProviders(context.Context, uuid.UUID, []string, int) ([]store.CodexPoolProviderSpan, error) {
	return nil, nil
}

func setupTraceReadToken(t *testing.T, ownerID string) string {
	t.Helper()
	token := "trace-read-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:      uuid.New(),
			Scopes:  []string{"operator.read"},
			OwnerID: ownerID,
		},
	})
	return token
}

func TestTracesFollowRequiresSessionOrAgent(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	tracing := &mockTracingStore{}
	mux := http.NewServeMux()
	NewTracesHandler(tracing).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/traces/follow", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(tracing.listOpts) != 0 {
		t.Fatalf("ListTraces called %d times, want 0", len(tracing.listOpts))
	}
}

func TestTracesFollowRejectsInvalidSince(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	tracing := &mockTracingStore{}
	mux := http.NewServeMux()
	NewTracesHandler(tracing).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/traces/follow?session_key=s1&since=not-a-time", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestTracesFollowScopesViewerAndParsesCursor(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	traceID := uuid.New()
	spanID := uuid.New()
	createdAt := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	since := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	tracing := &mockTracingStore{
		traces: []store.TraceData{{
			ID:         traceID,
			UserID:     "caller",
			SessionKey: "s1",
			Status:     store.TraceStatusRunning,
			CreatedAt:  createdAt,
		}},
		spans: map[uuid.UUID][]store.SpanData{
			traceID: {{ID: spanID, TraceID: traceID, SpanType: store.SpanTypeEvent, Status: store.SpanStatusCompleted}},
		},
	}
	mux := http.NewServeMux()
	NewTracesHandler(tracing).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/traces/follow?session_key=s1&user_id=other&status=running&since="+since.Format(time.RFC3339)+"&limit=999&include_spans=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(tracing.listOpts) != 1 {
		t.Fatalf("ListTraces calls = %d, want 1", len(tracing.listOpts))
	}
	opts := tracing.listOpts[0]
	if opts.UserID != "caller" {
		t.Fatalf("opts.UserID = %q, want caller", opts.UserID)
	}
	if opts.SessionKey != "s1" || opts.Status != store.TraceStatusRunning {
		t.Fatalf("opts = %+v, want session/status filters", opts)
	}
	if opts.ChangedAfter == nil || !opts.ChangedAfter.Equal(since) {
		t.Fatalf("ChangedAfter = %v, want %v", opts.ChangedAfter, since)
	}
	if opts.Limit != 200 {
		t.Fatalf("Limit = %d, want clamp to 200", opts.Limit)
	}

	var body struct {
		Traces         []store.TraceData           `json:"traces"`
		SpansByTraceID map[string][]store.SpanData `json:"spans_by_trace_id"`
		ServerTime     string                      `json:"server_time"`
		NextSince      string                      `json:"next_since"`
		Limit          int                         `json:"limit"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Traces) != 1 || body.Traces[0].ID != traceID {
		t.Fatalf("traces = %+v, want trace %s", body.Traces, traceID)
	}
	if got := body.SpansByTraceID[traceID.String()]; len(got) != 1 || got[0].ID != spanID {
		t.Fatalf("spans_by_trace_id = %+v, want span %s", body.SpansByTraceID, spanID)
	}
	if body.ServerTime == "" || body.NextSince == "" {
		t.Fatalf("server_time/next_since must be set: %+v", body)
	}
}

func TestTracesFollowRouteIsNotCapturedAsTraceID(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	tracing := &mockTracingStore{traces: []store.TraceData{}}
	mux := http.NewServeMux()
	NewTracesHandler(tracing).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/traces/follow?session_key=s1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		if strings.Contains(rec.Body.String(), "invalid trace") || strings.Contains(rec.Body.String(), "Invalid trace") {
			t.Fatalf("follow route was captured as trace ID: status=%d body=%s", rec.Code, rec.Body.String())
		}
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestTracesListParsesAdvancedFiltersAndScopesViewer(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	tracing := &mockTracingStore{}
	mux := http.NewServeMux()
	NewTracesHandler(tracing).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/traces?q=abc-123&from=2026-06-10T01:02:03Z&to=2026-06-11T04:05:06Z&user_id=other&status=error&channel=telegram&agent=helper&channel_query=ops&min_input_tokens=10&max_input_tokens=20&min_output_tokens=30&max_output_tokens=40&min_tool_calls=1&max_tool_calls=3&tool_name=web_%25&has_tool_calls=true&limit=25&offset=50", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(tracing.listOpts) != 1 {
		t.Fatalf("ListTraces calls = %d, want 1", len(tracing.listOpts))
	}
	opts := tracing.listOpts[0]
	if opts.UserID != "caller" {
		t.Fatalf("opts.UserID = %q, want caller", opts.UserID)
	}
	if opts.Query != "abc-123" || opts.AgentQuery != "helper" || opts.ChannelQuery != "ops" || opts.ToolName != "web_%" {
		t.Fatalf("search opts = %+v, want query/agent/channel/tool filters", opts)
	}
	if opts.From == nil || opts.From.Format(time.RFC3339) != "2026-06-10T01:02:03Z" {
		t.Fatalf("From = %v, want parsed RFC3339", opts.From)
	}
	if opts.To == nil || opts.To.Format(time.RFC3339) != "2026-06-11T04:05:06Z" {
		t.Fatalf("To = %v, want parsed RFC3339", opts.To)
	}
	if opts.MinInputTokens == nil || *opts.MinInputTokens != 10 ||
		opts.MaxInputTokens == nil || *opts.MaxInputTokens != 20 ||
		opts.MinOutputTokens == nil || *opts.MinOutputTokens != 30 ||
		opts.MaxOutputTokens == nil || *opts.MaxOutputTokens != 40 ||
		opts.MinToolCalls == nil || *opts.MinToolCalls != 1 ||
		opts.MaxToolCalls == nil || *opts.MaxToolCalls != 3 {
		t.Fatalf("range opts = %+v, want parsed token/tool ranges", opts)
	}
	if opts.HasToolCalls == nil || !*opts.HasToolCalls {
		t.Fatalf("HasToolCalls = %v, want true", opts.HasToolCalls)
	}
	if opts.Status != store.TraceStatusError || opts.Channel != "telegram" || opts.Limit != 25 || opts.Offset != 50 {
		t.Fatalf("existing opts = %+v, want status/channel/pagination preserved", opts)
	}
}

func TestTracesListRejectsInvalidDateRange(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	tracing := &mockTracingStore{}
	mux := http.NewServeMux()
	NewTracesHandler(tracing).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/traces?from=not-a-time", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(tracing.listOpts) != 0 {
		t.Fatalf("ListTraces called %d times, want 0", len(tracing.listOpts))
	}
}
