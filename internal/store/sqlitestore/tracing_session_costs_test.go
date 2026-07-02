//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteTracingStoreGetSessionCostsTenantScopedRootTraces(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	traces := NewSQLiteTracingStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	insertSessionCostTenant(t, db, tenantA, "tenant-a")
	insertSessionCostTenant(t, db, tenantB, "tenant-b")
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	rootA1 := createSessionCostTrace(t, traces, ctxA, "session-a", nil, 1.25, now)
	createSessionCostTrace(t, traces, ctxA, "session-a", nil, 0.75, now.Add(time.Second))
	createSessionCostTrace(t, traces, ctxA, "session-a", &rootA1, 99, now.Add(2*time.Second))
	createSessionCostTrace(t, traces, ctxB, "session-a", nil, 8, now.Add(3*time.Second))

	costs, err := traces.GetSessionCosts(ctxA, []string{"session-a", "session-a", " ", "missing"})
	if err != nil {
		t.Fatalf("GetSessionCosts: %v", err)
	}
	if got, want := costs["session-a"], 2.0; got != want {
		t.Fatalf("session-a cost = %v, want %v", got, want)
	}
	if _, ok := costs["missing"]; ok {
		t.Fatalf("missing session unexpectedly present: %#v", costs)
	}

	empty, err := traces.GetSessionCosts(context.Background(), []string{"session-a"})
	if err != nil {
		t.Fatalf("GetSessionCosts without tenant: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("nil-tenant result = %#v, want empty", empty)
	}
}

func TestSQLiteTracingStoreBatchUpdateTraceAggregatesIncludesToolUsageTokens(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	traces := NewSQLiteTracingStore(db)
	tenantID := uuid.Must(uuid.NewV7())
	insertSessionCostTenant(t, db, tenantID, "tenant-trace-aggregate")
	ctx := store.WithTenantID(context.Background(), tenantID)
	now := time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)
	traceID := createSessionCostTrace(t, traces, ctx, "session-a", nil, 0, now)

	llmSpanID := uuid.Must(uuid.NewV7())
	toolSpanID := uuid.Must(uuid.NewV7())
	llmCost := 0.25
	toolCost := 0.05
	if err := traces.CreateSpan(ctx, &store.SpanData{
		ID:           llmSpanID,
		TraceID:      traceID,
		SpanType:     store.SpanTypeLLMCall,
		StartTime:    now,
		Status:       store.SpanStatusCompleted,
		Provider:     "bailian",
		Model:        "qwen3.7-plus",
		InputTokens:  1000,
		OutputTokens: 200,
		TotalCost:    &llmCost,
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateSpan llm: %v", err)
	}
	if err := traces.CreateSpan(ctx, &store.SpanData{
		ID:           toolSpanID,
		TraceID:      traceID,
		SpanType:     store.SpanTypeToolCall,
		Name:         "read_image",
		StartTime:    now,
		Status:       store.SpanStatusCompleted,
		Provider:     "bailian",
		Model:        "qwen3.7-plus",
		InputTokens:  100,
		OutputTokens: 50,
		TotalCost:    &toolCost,
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateSpan tool: %v", err)
	}
	if err := traces.UpdateSpan(ctx, llmSpanID, map[string]any{"total_cost": llmCost}); err != nil {
		t.Fatalf("UpdateSpan llm: %v", err)
	}
	if err := traces.UpdateSpan(ctx, toolSpanID, map[string]any{"total_cost": toolCost}); err != nil {
		t.Fatalf("UpdateSpan tool: %v", err)
	}

	if err := traces.BatchUpdateTraceAggregates(ctx, traceID); err != nil {
		t.Fatalf("BatchUpdateTraceAggregates: %v", err)
	}
	got, err := traces.GetTrace(ctx, traceID)
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if got.TotalInputTokens != 1100 || got.TotalOutputTokens != 250 || math.Abs(got.TotalCost-0.30) > 0.0000001 || got.LLMCallCount != 1 || got.ToolCallCount != 1 {
		t.Fatalf("trace = input %d output %d cost %.2f llm %d tool %d, want 1100/250/0.30/1/1",
			got.TotalInputTokens, got.TotalOutputTokens, got.TotalCost, got.LLMCallCount, got.ToolCallCount)
	}
}

func insertSessionCostTenant(t *testing.T, db *sql.DB, id uuid.UUID, slug string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, 'active')`,
		id.String(), slug, slug,
	)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
}

func createSessionCostTrace(t *testing.T, traces *SQLiteTracingStore, ctx context.Context, sessionKey string, parentID *uuid.UUID, cost float64, ts time.Time) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	err := traces.CreateTrace(ctx, &store.TraceData{
		ID:            id,
		ParentTraceID: parentID,
		SessionKey:    sessionKey,
		StartTime:     ts,
		Status:        store.TraceStatusCompleted,
		TotalCost:     cost,
		CreatedAt:     ts,
	})
	if err != nil {
		t.Fatalf("CreateTrace: %v", err)
	}
	return id
}
