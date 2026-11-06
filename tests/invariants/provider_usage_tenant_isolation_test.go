//go:build integration

package invariants

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// INVARIANT: provider usage detail never mixes tenants. Tenant B cannot load
// tenant A's provider row (the handler answers 404), and stats queried for
// tenant B with the same provider name see none of tenant A's spans.
func TestTenantIsolation_ProviderCallStats(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	providers := pg.NewPGProviderStore(db, "")
	name := "inv-usage-" + uuid.NewString()[:8]
	provA := &store.LLMProviderData{Name: name, ProviderType: "openai_compat", APIBase: "http://127.0.0.1:0", Enabled: true}
	if err := providers.CreateProvider(ctxA, provA); err != nil {
		t.Fatalf("create provider A: %v", err)
	}
	t.Cleanup(func() { _ = providers.DeleteProvider(ctxA, provA.ID) })

	tracing := pg.NewPGTracingStore(db)
	now := time.Now().UTC()
	seedProviderSpan(t, ctxA, tracing, tenantA, name, now.Add(-time.Hour), store.SpanStatusCompleted, "")
	seedProviderSpan(t, ctxA, tracing, tenantA, name, now.Add(-30*time.Minute), store.SpanStatusError, "HTTP 429: secret-body")

	q := store.ProviderCallStatsQuery{Provider: name, From: now.Add(-24 * time.Hour), To: now.Add(time.Minute), ErrorLimit: 10}

	q.TenantID = tenantA
	statsA, err := tracing.GetProviderCallStats(ctxA, q)
	if err != nil {
		t.Fatalf("stats A: %v", err)
	}
	if statsA.Calls != 2 || statsA.Errors != 1 {
		t.Fatalf("tenant A stats = %+v, want 2 calls / 1 error", statsA)
	}

	// INVARIANT: tenant B cannot resolve tenant A's provider id.
	got, err := providers.GetProvider(ctxB, provA.ID)
	assertAccessDenied(t, got, err, "tenant B loading tenant A's provider")

	// INVARIANT: tenant B's stats for the same provider name are empty.
	q.TenantID = tenantB
	statsB, err := tracing.GetProviderCallStats(ctxB, q)
	if err != nil {
		t.Fatalf("stats B: %v", err)
	}
	if statsB.Calls != 0 || statsB.Errors != 0 || len(statsB.RecentErrors) != 0 {
		t.Errorf("INVARIANT VIOLATION: tenant B sees tenant A's spans: %+v", statsB)
	}
}

func seedProviderSpan(t *testing.T, ctx context.Context, ts *pg.PGTracingStore, tenantID uuid.UUID, provider string, start time.Time, status, errText string) {
	t.Helper()
	trace := &store.TraceData{StartTime: start, Status: store.TraceStatusCompleted, CreatedAt: start}
	if err := ts.CreateTrace(ctx, trace); err != nil {
		t.Fatalf("create trace: %v", err)
	}
	span := &store.SpanData{
		TraceID:    trace.ID,
		TenantID:   tenantID,
		SpanType:   store.SpanTypeLLMCall,
		Name:       "llm",
		StartTime:  start,
		DurationMS: 1000,
		Status:     status,
		Provider:   provider,
		Model:      "m",
		Error:      errText,
		CreatedAt:  start,
	}
	if err := ts.CreateSpan(ctx, span); err != nil {
		t.Fatalf("create span: %v", err)
	}
}
