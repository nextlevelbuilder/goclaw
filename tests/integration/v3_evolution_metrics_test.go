//go:build integration

package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestV3EvolutionMetrics_RecordAndQuery(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGEvolutionMetricsStore(db)

	// Record a tool metric.
	value, _ := json.Marshal(map[string]any{"success": true, "duration_ms": 42})
	metric := store.EvolutionMetric{
		ID:         uuid.New(),
		TenantID:   tenantID,
		AgentID:    agentID,
		SessionKey: "test-session",
		MetricType: store.MetricTool,
		MetricKey:  "exec",
		Value:      value,
	}
	if err := ms.RecordMetric(ctx, metric); err != nil {
		t.Fatalf("RecordMetric: %v", err)
	}

	// Query it back.
	results, err := ms.QueryMetrics(ctx, agentID, store.MetricTool, time.Now().Add(-1*time.Hour), 10)
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(results))
	}
	if results[0].MetricKey != "exec" {
		t.Errorf("expected MetricKey=exec, got %q", results[0].MetricKey)
	}
}

func TestV3EvolutionMetrics_AggregateTools(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGEvolutionMetricsStore(db)

	// Record 3 tool calls: 2 success, 1 failure.
	for i, success := range []bool{true, true, false} {
		value, _ := json.Marshal(map[string]any{"success": success, "duration_ms": 100 + i*50})
		if err := ms.RecordMetric(ctx, store.EvolutionMetric{
			ID: uuid.New(), TenantID: tenantID, AgentID: agentID,
			SessionKey: "s1", MetricType: store.MetricTool, MetricKey: "web_search",
			Value: value,
		}); err != nil {
			t.Fatalf("RecordMetric #%d: %v", i, err)
		}
	}

	aggs, err := ms.AggregateToolMetrics(ctx, agentID, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("AggregateToolMetrics: %v", err)
	}
	if len(aggs) != 1 {
		t.Fatalf("expected 1 aggregate, got %d", len(aggs))
	}
	if aggs[0].CallCount != 3 {
		t.Errorf("expected 3 calls, got %d", aggs[0].CallCount)
	}
	// 2/3 success = ~0.667
	if aggs[0].SuccessRate < 0.6 || aggs[0].SuccessRate > 0.7 {
		t.Errorf("expected ~0.667 success rate, got %f", aggs[0].SuccessRate)
	}
}

func TestV3EvolutionMetrics_Cleanup(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGEvolutionMetricsStore(db)

	// Record an old metric.
	value, _ := json.Marshal(map[string]any{"success": true})
	if err := ms.RecordMetric(ctx, store.EvolutionMetric{
		ID: uuid.New(), TenantID: tenantID, AgentID: agentID,
		SessionKey: "old", MetricType: store.MetricTool, MetricKey: "old_tool",
		Value: value,
	}); err != nil {
		t.Fatalf("RecordMetric: %v", err)
	}

	// Cleanup with future threshold (should delete everything).
	deleted, err := ms.Cleanup(ctx, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted < 1 {
		t.Errorf("expected >=1 deleted, got %d", deleted)
	}
}
