package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type emptyUsageSnapshotStore struct{}

func (emptyUsageSnapshotStore) UpsertSnapshots(context.Context, []store.UsageSnapshot) error {
	return nil
}

func (emptyUsageSnapshotStore) GetTimeSeries(context.Context, store.SnapshotQuery) ([]store.SnapshotTimeSeries, error) {
	return nil, nil
}

func (emptyUsageSnapshotStore) GetBreakdown(context.Context, store.SnapshotQuery) ([]store.SnapshotBreakdown, error) {
	return nil, nil
}

func (emptyUsageSnapshotStore) GetLatestBucket(context.Context) (*time.Time, error) {
	return nil, nil
}

func TestUsageSummaryIncludesLiveCurrentHourCost(t *testing.T) {
	db, tenantID := usageLiveTestDB(t)
	insertUsageLiveTrace(t, db, tenantID, time.Now().UTC())

	h := NewUsageHandler(emptyUsageSnapshotStore{}, nil, db)
	req := httptest.NewRequest(http.MethodGet, "/v1/usage/summary?period=24h", nil)
	req = req.WithContext(store.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.handleSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Current usageSummary `json:"current"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Current.Cost != 0.25 {
		t.Fatalf("current cost = %v, want 0.25", body.Current.Cost)
	}
	if body.Current.InputTokens != 1100 || body.Current.OutputTokens != 250 {
		t.Fatalf("tokens = %d/%d, want 1100/250", body.Current.InputTokens, body.Current.OutputTokens)
	}
	if body.Current.Requests != 1 || body.Current.LLMCalls != 1 || body.Current.ToolCalls != 2 {
		t.Fatalf("counts = requests %d llm %d tools %d, want 1/1/2", body.Current.Requests, body.Current.LLMCalls, body.Current.ToolCalls)
	}
}

func TestUsageSummaryHandlesEmptyLiveCurrentHour(t *testing.T) {
	db, tenantID := usageLiveTestDB(t)

	h := NewUsageHandler(emptyUsageSnapshotStore{}, nil, db)
	req := httptest.NewRequest(http.MethodGet, "/v1/usage/summary?period=24h", nil)
	req = req.WithContext(store.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.handleSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Current usageSummary `json:"current"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Current.Cost != 0 || body.Current.Requests != 0 || body.Current.LLMCalls != 0 {
		t.Fatalf("current = %+v, want empty live usage", body.Current)
	}
}

func TestUsageBreakdownIncludesLiveCurrentHourProviderModelCost(t *testing.T) {
	db, tenantID := usageLiveTestDB(t)
	now := time.Now().UTC()
	insertUsageLiveTrace(t, db, tenantID, now)

	h := NewUsageHandler(emptyUsageSnapshotStore{}, nil, db)
	target := fmt.Sprintf(
		"/v1/usage/breakdown?from=%s&to=%s&group_by=provider_model",
		now.Add(-time.Hour).Format(time.RFC3339),
		now.Add(time.Hour).Format(time.RFC3339),
	)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(store.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.handleBreakdown(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Rows []store.SnapshotBreakdown `json:"rows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Rows) != 1 {
		t.Fatalf("rows = %+v, want one provider/model row", body.Rows)
	}
	row := body.Rows[0]
	if row.Key != "bailian/qwen3.7-plus" {
		t.Fatalf("key = %q, want bailian/qwen3.7-plus", row.Key)
	}
	if row.TotalCost != 0.25 || row.InputTokens != 1100 || row.OutputTokens != 250 || row.LLMCallCount != 1 {
		t.Fatalf("row = %+v, want live cost/tokens/llm count", row)
	}
}

func usageLiveTestDB(t *testing.T) (*sql.DB, uuid.UUID) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := []string{
		`CREATE TABLE traces (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			agent_id TEXT,
			user_id TEXT,
			start_time TEXT NOT NULL,
			duration_ms INTEGER,
			status TEXT,
			channel TEXT,
			parent_trace_id TEXT,
			tool_call_count INTEGER
		)`,
		`CREATE TABLE spans (
			id TEXT PRIMARY KEY,
			trace_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			span_type TEXT NOT NULL,
			provider TEXT,
			model TEXT,
			input_tokens INTEGER,
			output_tokens INTEGER,
			total_cost REAL,
			metadata TEXT,
			start_time TEXT
		)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return db, uuid.Must(uuid.NewV7())
}

func insertUsageLiveTrace(t *testing.T, db *sql.DB, tenantID uuid.UUID, start time.Time) {
	t.Helper()
	traceID := uuid.Must(uuid.NewV7())
	spanID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO traces (
			id, tenant_id, agent_id, user_id, start_time, duration_ms, status, channel, tool_call_count
		) VALUES (?, ?, ?, 'user-a', ?, 1200, 'completed', 'web', 2)`,
		traceID, tenantID, uuid.Must(uuid.NewV7()), start,
	); err != nil {
		t.Fatalf("insert trace: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO spans (
			id, trace_id, tenant_id, span_type, provider, model, input_tokens, output_tokens, total_cost, metadata, start_time
		) VALUES (?, ?, ?, 'llm_call', 'bailian', 'qwen3.7-plus', 1000, 200, 0.25, ?, ?)`,
		spanID, traceID, tenantID,
		`{"cache_read_tokens":7,"cache_creation_tokens":11,"thinking_tokens":13}`,
		start,
	); err != nil {
		t.Fatalf("insert span: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO spans (
			id, trace_id, tenant_id, span_type, provider, model, input_tokens, output_tokens, total_cost, metadata, start_time
		) VALUES (?, ?, ?, 'tool_call', 'bailian', 'qwen3.7-plus', 100, 50, 0, ?, ?)`,
		uuid.Must(uuid.NewV7()), traceID, tenantID,
		`{"cache_read_tokens":0,"cache_creation_tokens":0,"thinking_tokens":0}`,
		start,
	); err != nil {
		t.Fatalf("insert tool span: %v", err)
	}
}
