//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// PG provider call stats: nearest-rank percentiles over finished calls in range,
// running spans, other providers and out-of-range spans excluded, newest errors first.
func TestPGProviderCallStats(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTracingStore(db)

	name := "stats-" + uuid.NewString()[:8]
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	addSpan := func(provider string, start time.Time, durMS int, status, errText string) {
		t.Helper()
		trace := &store.TraceData{StartTime: start, Status: store.TraceStatusCompleted, CreatedAt: start}
		if err := ts.CreateTrace(ctx, trace); err != nil {
			t.Fatalf("create trace: %v", err)
		}
		if err := ts.CreateSpan(ctx, &store.SpanData{
			TraceID: trace.ID, TenantID: tenantID, SpanType: store.SpanTypeLLMCall, Name: "llm",
			StartTime: start, DurationMS: durMS, Status: status, Provider: provider, Model: "m",
			Error: errText, CreatedAt: start,
		}); err != nil {
			t.Fatalf("create span: %v", err)
		}
	}

	// 20 finished calls: 100, 200, ... 2000 ms; the last two failed.
	for i := 1; i <= 20; i++ {
		status, errText := store.SpanStatusCompleted, ""
		if i > 18 {
			status, errText = store.SpanStatusError, "HTTP 429: slow down"
		}
		addSpan(name, base.Add(time.Duration(i)*time.Minute), i*100, status, errText)
	}
	addSpan(name, base.Add(30*time.Minute), 99999, store.SpanStatusRunning, "")      // running: excluded
	addSpan("other-"+name, base.Add(time.Minute), 99999, store.SpanStatusCompleted, "") // other provider
	addSpan(name, base.Add(-48*time.Hour), 99999, store.SpanStatusError, "boom")        // out of range

	stats, err := ts.GetProviderCallStats(ctx, store.ProviderCallStatsQuery{
		TenantID: tenantID, Provider: name, From: base, To: base.Add(time.Hour), ErrorLimit: 10,
	})
	if err != nil {
		t.Fatalf("GetProviderCallStats: %v", err)
	}
	if stats.Calls != 20 || stats.Errors != 2 {
		t.Fatalf("calls/errors = %d/%d, want 20/2", stats.Calls, stats.Errors)
	}
	if stats.LatencyP50MS != 1000 || stats.LatencyP95MS != 1900 {
		t.Errorf("p50/p95 = %v/%v, want 1000/1900", stats.LatencyP50MS, stats.LatencyP95MS)
	}
	if len(stats.RecentErrors) != 2 || !stats.RecentErrors[0].At.After(stats.RecentErrors[1].At) {
		t.Errorf("recent errors not newest first: %+v", stats.RecentErrors)
	}

	empty, err := ts.GetProviderCallStats(ctx, store.ProviderCallStatsQuery{
		TenantID: tenantID, Provider: name, From: base.Add(-10 * time.Hour), To: base.Add(-9 * time.Hour), ErrorLimit: 10,
	})
	if err != nil {
		t.Fatalf("empty range: %v", err)
	}
	if empty.Calls != 0 || empty.LatencyP95MS != 0 || empty.RecentErrors == nil || len(empty.RecentErrors) != 0 {
		t.Errorf("empty range = %+v, want zeros and an empty list", empty)
	}
}
