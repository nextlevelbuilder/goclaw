//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteProviderCallStats(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	ts := NewSQLiteTracingStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	insertSessionCostTenant(t, db, tenantA, "stats-a")
	insertSessionCostTenant(t, db, tenantB, "stats-b")
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	base := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	addSpan := func(ctx context.Context, tenant uuid.UUID, provider string, start time.Time, durMS int, status, errText string) {
		t.Helper()
		trace := &store.TraceData{StartTime: start, Status: store.TraceStatusCompleted, CreatedAt: start}
		if err := ts.CreateTrace(ctx, trace); err != nil {
			t.Fatalf("CreateTrace: %v", err)
		}
		if err := ts.CreateSpan(ctx, &store.SpanData{
			TraceID: trace.ID, TenantID: tenant, SpanType: store.SpanTypeLLMCall, Name: "llm",
			StartTime: start, DurationMS: durMS, Status: status, Provider: provider, Model: "m",
			Error: errText, CreatedAt: start,
		}); err != nil {
			t.Fatalf("CreateSpan: %v", err)
		}
	}

	// 20 finished calls: 100 ... 2000 ms, inserted out of order; the last two failed.
	for _, i := range []int{7, 20, 1, 13, 2, 19, 3, 18, 4, 17, 5, 16, 6, 15, 8, 14, 9, 12, 10, 11} {
		status, errText := store.SpanStatusCompleted, ""
		if i > 18 {
			status, errText = store.SpanStatusError, "HTTP 429: slow down"
		}
		addSpan(ctxA, tenantA, "dashscope", base.Add(time.Duration(i)*time.Minute), i*100, status, errText)
	}
	addSpan(ctxA, tenantA, "dashscope", base.Add(30*time.Minute), 99999, store.SpanStatusRunning, "")
	addSpan(ctxA, tenantA, "other", base.Add(time.Minute), 99999, store.SpanStatusCompleted, "")
	addSpan(ctxA, tenantA, "dashscope", base.Add(-48*time.Hour), 99999, store.SpanStatusError, "boom")
	addSpan(ctxB, tenantB, "dashscope", base.Add(time.Minute), 99999, store.SpanStatusError, "tenant-b")

	q := store.ProviderCallStatsQuery{TenantID: tenantA, Provider: "dashscope", From: base, To: base.Add(time.Hour), ErrorLimit: 10}
	stats, err := ts.GetProviderCallStats(ctxA, q)
	if err != nil {
		t.Fatalf("GetProviderCallStats: %v", err)
	}
	if stats.Calls != 20 || stats.Errors != 2 {
		t.Fatalf("calls/errors = %d/%d, want 20/2", stats.Calls, stats.Errors)
	}
	if stats.LatencyP50MS != 1000 || stats.LatencyP95MS != 1900 {
		t.Errorf("p50/p95 = %v/%v, want 1000/1900", stats.LatencyP50MS, stats.LatencyP95MS)
	}
	if len(stats.RecentErrors) != 2 || !stats.RecentErrors[0].At.Equal(base.Add(20*time.Minute)) {
		t.Errorf("recent errors = %+v, want 2 newest first", stats.RecentErrors)
	}

	q.TenantID = tenantB
	statsB, err := ts.GetProviderCallStats(ctxB, q)
	if err != nil {
		t.Fatalf("tenant B: %v", err)
	}
	if statsB.Calls != 1 || statsB.RecentErrors[0].Error != "tenant-b" {
		t.Errorf("tenant B stats = %+v, want only its own span", statsB)
	}

	q.TenantID = tenantA
	q.From, q.To = base.Add(-10*time.Hour), base.Add(-9*time.Hour)
	empty, err := ts.GetProviderCallStats(ctxA, q)
	if err != nil {
		t.Fatalf("empty range: %v", err)
	}
	if empty.Calls != 0 || empty.LatencyP50MS != 0 || empty.RecentErrors == nil || len(empty.RecentErrors) != 0 {
		t.Errorf("empty range = %+v, want zeros and an empty list", empty)
	}
}
