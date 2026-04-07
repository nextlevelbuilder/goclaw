//go:build integration

package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestV3EvolutionSuggestions_CRUD(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGEvolutionSuggestionStore(db)

	// Create suggestion.
	sg := store.EvolutionSuggestion{
		ID:             uuid.New(),
		TenantID:       tenantID,
		AgentID:        agentID,
		SuggestionType: store.SuggestThreshold,
		Suggestion:     "Raise retrieval threshold",
		Rationale:      "Low usage rate",
		Parameters:     json.RawMessage(`{"source":"mem","current_usage_rate":0.15}`),
		Status:         "pending",
	}
	if err := ss.CreateSuggestion(ctx, sg); err != nil {
		t.Fatalf("CreateSuggestion: %v", err)
	}

	// Get by ID.
	got, err := ss.GetSuggestion(ctx, sg.ID)
	if err != nil {
		t.Fatalf("GetSuggestion: %v", err)
	}
	if got == nil {
		t.Fatal("GetSuggestion returned nil")
	}
	if got.Suggestion != sg.Suggestion {
		t.Errorf("suggestion text = %q, want %q", got.Suggestion, sg.Suggestion)
	}

	// List pending.
	list, err := ss.ListSuggestions(ctx, agentID, "pending", 10)
	if err != nil {
		t.Fatalf("ListSuggestions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(list))
	}

	// Update parameters (baseline persist).
	newParams, _ := json.Marshal(map[string]any{
		"source":             "mem",
		"current_usage_rate": 0.15,
		"_baseline":          map[string]any{"retrieval_threshold": 0.3},
	})
	if err := ss.UpdateSuggestionParameters(ctx, sg.ID, newParams); err != nil {
		t.Fatalf("UpdateSuggestionParameters: %v", err)
	}

	// Update status to applied.
	if err := ss.UpdateSuggestionStatus(ctx, sg.ID, "applied", "auto-adapt"); err != nil {
		t.Fatalf("UpdateSuggestionStatus: %v", err)
	}

	// Verify status + params updated.
	updated, _ := ss.GetSuggestion(ctx, sg.ID)
	if updated.Status != "applied" {
		t.Errorf("status = %q, want applied", updated.Status)
	}
	var params map[string]any
	json.Unmarshal(updated.Parameters, &params)
	if _, ok := params["_baseline"]; !ok {
		t.Error("_baseline key missing after UpdateSuggestionParameters")
	}

	// List applied (pending should be empty now).
	pending, _ := ss.ListSuggestions(ctx, agentID, "pending", 10)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after status update, got %d", len(pending))
	}
}

func TestV3EvolutionSuggestions_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ss := pg.NewPGEvolutionSuggestionStore(db)

	// Create suggestion in tenant A.
	sg := store.EvolutionSuggestion{
		ID: uuid.New(), TenantID: tenantA, AgentID: agentA,
		SuggestionType: store.SuggestToolOrder,
		Suggestion: "test", Rationale: "test", Status: "pending",
	}
	ss.CreateSuggestion(ctxA, sg)

	// Tenant B should not see it.
	got, err := ss.GetSuggestion(ctxB, sg.ID)
	if err != nil {
		t.Fatalf("GetSuggestion: %v", err)
	}
	if got != nil {
		t.Error("tenant B should NOT see tenant A's suggestion")
	}
}

func TestV3SuggestionEngine_Pipeline(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGEvolutionMetricsStore(db)
	ss := pg.NewPGEvolutionSuggestionStore(db)

	// Seed enough tool metrics to trigger ToolFailureRule.
	// 25 calls with 0% success rate → should trigger suggestion.
	for i := 0; i < 25; i++ {
		value, _ := json.Marshal(map[string]any{"success": false, "duration_ms": 500})
		ms.RecordMetric(ctx, store.EvolutionMetric{
			ID: uuid.New(), TenantID: tenantID, AgentID: agentID,
			SessionKey: "pipeline-test", MetricType: store.MetricTool, MetricKey: "broken_tool",
			Value: value,
		})
	}

	// Seed retrieval metrics (low usage to trigger LowRetrievalUsageRule).
	for i := 0; i < 55; i++ {
		value, _ := json.Marshal(map[string]any{"used_in_reply": false, "top_score": 0.5})
		ms.RecordMetric(ctx, store.EvolutionMetric{
			ID: uuid.New(), TenantID: tenantID, AgentID: agentID,
			SessionKey: "pipeline-test", MetricType: store.MetricRetrieval, MetricKey: "auto_inject",
			Value: value,
		})
	}

	// Run suggestion engine.
	engine := agent.NewSuggestionEngine(ms, ss)
	created, err := engine.Analyze(ctx, agentID)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(created) == 0 {
		t.Fatal("expected at least 1 suggestion, got 0")
	}

	// Verify suggestions are in DB.
	all, _ := ss.ListSuggestions(ctx, agentID, "pending", 20)
	if len(all) == 0 {
		t.Fatal("no pending suggestions in DB after Analyze")
	}

	// Run again — should NOT create duplicates (dedup by type).
	created2, _ := engine.Analyze(ctx, agentID)
	if len(created2) != 0 {
		t.Errorf("second Analyze should create 0 new suggestions (dedup), got %d", len(created2))
	}

	// Verify count unchanged.
	all2, _ := ss.ListSuggestions(ctx, agentID, "pending", 20)
	if len(all2) != len(all) {
		t.Errorf("suggestion count changed after dedup run: %d → %d", len(all), len(all2))
	}

	// Cleanup metrics manually (test-specific, beyond seedTenantAgent cleanup).
	_, _ = ms.Cleanup(ctx, time.Now().Add(1*time.Hour))
}
