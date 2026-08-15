package pg

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestPGUsageEventStoreBackfillUsageEventCosts(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, agentID := seedTenantAndAgent(t, db)
	providerID := uuid.Must(uuid.NewV7())
	traceID := uuid.Must(uuid.NewV7())
	spanID := uuid.Must(uuid.NewV7())
	start := time.Date(2026, 7, 3, 2, 15, 0, 0, time.UTC)
	modelID := "gpt-usage-event-backfill-" + spanID.String()[:8]
	catalogModelID := "openai/" + modelID

	t.Cleanup(func() {
		db.Exec(`DELETE FROM usage_event_rollups WHERE tenant_id=$1`, tenantID)
		db.Exec(`DELETE FROM usage_events WHERE tenant_id=$1`, tenantID)
		db.Exec(`DELETE FROM spans WHERE id=$1`, spanID)
		db.Exec(`DELETE FROM traces WHERE id=$1`, traceID)
		db.Exec(`DELETE FROM llm_providers WHERE id=$1`, providerID)
		db.Exec(`DELETE FROM usage_pricing_catalog WHERE model_id=$1`, catalogModelID)
	})

	if _, err := db.Exec(
		`INSERT INTO llm_providers (id, tenant_id, name, provider_type, api_key, enabled)
		 VALUES ($1,$2,'cppai','openai_compat','test',true)`,
		providerID, tenantID,
	); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	inputPrice := "0.000005"
	outputPrice := "0.000030"
	if _, err := NewPGUsageCapStore(db).UpsertPricingCatalog(context.Background(), []store.UsagePricingCatalogEntry{{
		ModelID:          catalogModelID,
		CanonicalModelID: catalogModelID,
		Pricing: store.UsagePricingFields{
			Input:  &inputPrice,
			Output: &outputPrice,
		},
		SyncedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("upsert pricing catalog: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO traces (id, tenant_id, agent_id, start_time, created_at, status)
		 VALUES ($1,$2,$3,$4,$4,'completed')`,
		traceID, tenantID, agentID, start,
	); err != nil {
		t.Fatalf("insert trace: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO spans (
			id, trace_id, tenant_id, agent_id, span_type, start_time, created_at, status,
			name, provider, model, input_tokens, output_tokens, total_cost
		) VALUES ($1,$2,$3,$4,'tool_call',$5,$5,'completed','read_image','cppai',$6,1000,500,0)`,
		spanID, traceID, tenantID, agentID, start, modelID,
	); err != nil {
		t.Fatalf("insert span: %v", err)
	}
	eventStore := NewPGUsageEventStore(db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	if err := eventStore.InsertEvent(ctx, &store.UsageEvent{
		TenantID:     tenantID,
		EventTime:    start,
		BucketHour:   start.Truncate(time.Hour),
		EventType:    store.UsageEventTypeToolCall,
		ResourceType: store.UsageResourceTypeTool,
		ResourceName: "read_image",
		ResourceID:   "read_image",
		Source:       store.UsageSourceToolCall,
		AgentID:      &agentID,
		TraceID:      &traceID,
		SpanID:       &spanID,
		Provider:     "cppai",
		Model:        modelID,
		Status:       "completed",
		InputTokens:  1000,
		OutputTokens: 500,
		TotalTokens:  1500,
		CallCount:    1,
	}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	traceStats, err := NewPGTracingStore(db).BackfillLLMCosts(context.Background())
	if err != nil {
		t.Fatalf("BackfillLLMCosts: %v", err)
	}
	if traceStats.SpanRowsUpdated == 0 {
		t.Fatalf("trace stats = %+v, want tool span cost updated", traceStats)
	}

	eventStats, err := eventStore.BackfillUsageEventCosts(context.Background())
	if err != nil {
		t.Fatalf("BackfillUsageEventCosts: %v", err)
	}
	if eventStats.EventRowsUpdated != 1 || len(eventStats.RollupBuckets) != 1 {
		t.Fatalf("event stats = %+v, want 1 event and 1 rollup bucket", eventStats)
	}

	var eventCost float64
	if err := db.QueryRow(`SELECT cost_usd FROM usage_events WHERE span_id=$1`, spanID).Scan(&eventCost); err != nil {
		t.Fatalf("query usage event cost: %v", err)
	}
	if math.Abs(eventCost-0.02) > 0.0000001 {
		t.Fatalf("event cost = %.8f, want 0.02000000", eventCost)
	}
	summary, err := eventStore.GetEventSummary(ctx, store.UsageEventQuery{
		From:         start.Add(-time.Hour),
		To:           start.Add(time.Hour),
		ResourceType: store.UsageResourceTypeTool,
	})
	if err != nil {
		t.Fatalf("GetEventSummary: %v", err)
	}
	if math.Abs(summary.CostUSD-0.02) > 0.0000001 {
		t.Fatalf("summary cost = %.8f, want 0.02000000", summary.CostUSD)
	}
}
