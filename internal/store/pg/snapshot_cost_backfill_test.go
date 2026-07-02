package pg

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestPGSnapshotStoreBackfillSnapshotCosts(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, agentID := seedTenantAndAgent(t, db)
	providerID := uuid.Must(uuid.NewV7())
	hour := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)

	t.Cleanup(func() {
		db.Exec(`DELETE FROM usage_snapshots WHERE tenant_id=$1 AND bucket_hour=$2`, tenantID, hour)
		db.Exec(`DELETE FROM llm_providers WHERE id=$1`, providerID)
		db.Exec(`DELETE FROM usage_pricing_catalog WHERE model_id=$1`, "qwen/qwen3.7-plus")
	})

	if _, err := db.Exec(
		`INSERT INTO llm_providers (id, tenant_id, name, provider_type, api_key, enabled)
		 VALUES ($1,$2,'bailian','bailian','test',true)`,
		providerID, tenantID,
	); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	inputPrice := "0.000001"
	outputPrice := "0.000002"
	cacheReadPrice := "0.0000001"
	cacheWritePrice := "0.0000002"
	if _, err := NewPGUsageCapStore(db).UpsertPricingCatalog(context.Background(), []store.UsagePricingCatalogEntry{{
		ModelID:          "qwen/qwen3.7-plus",
		CanonicalModelID: "qwen/qwen3.7-plus",
		Pricing: store.UsagePricingFields{
			Input:      &inputPrice,
			Output:     &outputPrice,
			CacheRead:  &cacheReadPrice,
			CacheWrite: &cacheWritePrice,
		},
		SyncedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("upsert pricing catalog: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO usage_snapshots (
			bucket_hour, agent_id, provider, model, channel,
			input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, thinking_tokens,
			total_cost, request_count, llm_call_count, tool_call_count,
			error_count, unique_users, avg_duration_ms, tenant_id
		) VALUES ($1,$2,'bailian','qwen3.7-plus','web',1000,500,10,5,100,0,0,1,0,0,0,0,$3)`,
		hour, agentID, tenantID,
	); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	stats, err := NewPGSnapshotStore(db).BackfillSnapshotCosts(context.Background())
	if err != nil {
		t.Fatalf("BackfillSnapshotCosts: %v", err)
	}
	if stats.SnapshotRowsUpdated == 0 {
		t.Fatalf("stats = %+v, want updated snapshot", stats)
	}

	var cost float64
	if err := db.QueryRow(`SELECT total_cost FROM usage_snapshots WHERE tenant_id=$1 AND bucket_hour=$2 AND provider='bailian'`, tenantID, hour).Scan(&cost); err != nil {
		t.Fatalf("query snapshot cost: %v", err)
	}
	if math.Abs(cost-0.001987) > 0.0000001 {
		t.Fatalf("snapshot cost = %.8f, want 0.001987", cost)
	}

	points, err := NewPGSnapshotStore(db).GetTimeSeries(store.WithTenantID(context.Background(), tenantID), store.SnapshotQuery{
		From:    hour.Add(-time.Hour),
		To:      hour.Add(time.Hour),
		GroupBy: "hour",
	})
	if err != nil {
		t.Fatalf("GetTimeSeries: %v", err)
	}
	if len(points) != 1 || math.Abs(points[0].TotalCost-0.001987) > 0.0000001 {
		t.Fatalf("points = %+v, want backfilled cost", points)
	}
}
