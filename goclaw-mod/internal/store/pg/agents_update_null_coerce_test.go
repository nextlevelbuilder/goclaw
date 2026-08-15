package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestIsEmptyOrNullJSONUpdate(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{name: "nil interface", in: nil, want: true},
		{name: "nil raw message", in: json.RawMessage(nil), want: true},
		{name: "json null raw message", in: json.RawMessage(`null`), want: true},
		{name: "json null bytes", in: []byte(` null `), want: true},
		{name: "empty string", in: "", want: true},
		{name: "json null string", in: " null ", want: true},
		{name: "object raw message", in: json.RawMessage(`{"enabled":true}`), want: false},
		{name: "empty object", in: []byte(`{}`), want: false},
		{name: "map", in: map[string]any{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyOrNullJSONUpdate(tt.in); got != tt.want {
				t.Fatalf("isEmptyOrNullJSONUpdate(%T) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPGAgentStoreSyncsMonthlyBudgetUsageCap(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := tenantScopedCtx(tenantID)
	agentStore := NewPGAgentStore(db)
	budgetCents := 123
	agentID := uuid.New()
	agent := &store.AgentData{
		BaseModel: store.BaseModel{ID: agentID},
		TenantID:  tenantID, AgentKey: "budget-agent-" + agentID.String(),
		OwnerID: "owner", Provider: "openai", Model: "gpt-test",
		AgentType: store.AgentTypePredefined, Status: store.AgentStatusActive,
		BudgetMonthlyCents: &budgetCents,
	}
	if err := agentStore.Create(ctx, agent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertAgentBudgetPolicy(t, db, tenantID, agentID, 1230000)
	var generatedPolicyID uuid.UUID
	if err := db.QueryRowContext(context.Background(),
		`SELECT id FROM usage_cap_policies
		 WHERE tenant_id=$1 AND agent_id=$2 AND source=$3`,
		tenantID, agentID, store.UsageCapSourceAgentBudget).Scan(&generatedPolicyID); err != nil {
		t.Fatalf("query generated policy id: %v", err)
	}
	if err := NewPGUsageCapStore(db).DeleteUsageCapPolicy(ctx, tenantID, generatedPolicyID); err == nil {
		t.Fatal("DeleteUsageCapPolicy deleted generated agent budget policy")
	} else if !errors.Is(err, store.ErrUsageCapPolicyManaged) {
		t.Fatalf("DeleteUsageCapPolicy error = %v, want managed policy error", err)
	}
	disablePolicy := false
	if _, err := NewPGUsageCapStore(db).UpdateUsageCapPolicy(ctx, tenantID, generatedPolicyID, store.UsageCapPolicyPatch{
		Enabled: &disablePolicy,
	}); err == nil {
		t.Fatal("UpdateUsageCapPolicy updated generated agent budget policy")
	} else if !errors.Is(err, store.ErrUsageCapPolicyManaged) {
		t.Fatalf("UpdateUsageCapPolicy error = %v, want managed policy error", err)
	}
	assertAgentBudgetPolicy(t, db, tenantID, agentID, 1230000)

	if err := agentStore.Update(ctx, agentID, map[string]any{"budget_monthly_cents": float64(456)}); err != nil {
		t.Fatalf("Update budget: %v", err)
	}
	assertAgentBudgetPolicy(t, db, tenantID, agentID, 4560000)

	if err := agentStore.Update(ctx, agentID, map[string]any{"budget_monthly_cents": nil}); err != nil {
		t.Fatalf("Clear budget: %v", err)
	}
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM usage_cap_policies
		 WHERE tenant_id=$1 AND agent_id=$2 AND source=$3`,
		tenantID, agentID, store.UsageCapSourceAgentBudget).Scan(&count); err != nil {
		t.Fatalf("count cleared policy: %v", err)
	}
	if count != 0 {
		t.Fatalf("agent budget policy count after clear = %d, want 0", count)
	}
}

func assertAgentBudgetPolicy(t *testing.T, db rowQueryer, tenantID, agentID uuid.UUID, wantCostMicros int64) {
	t.Helper()
	var windowKey, source string
	var costMicros int64
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(window_key), ''), COALESCE(MAX(source), ''), COALESCE(MAX(max_cost_micros), 0)
		 FROM usage_cap_policies
		 WHERE tenant_id=$1 AND agent_id=$2 AND source=$3`,
		tenantID, agentID, store.UsageCapSourceAgentBudget).Scan(&count, &windowKey, &source, &costMicros); err != nil {
		t.Fatalf("query agent budget policy: %v", err)
	}
	if count != 1 {
		t.Fatalf("agent budget policy count = %d, want 1", count)
	}
	if windowKey != store.UsageCapWindowMonth {
		t.Fatalf("window_key = %q, want month", windowKey)
	}
	if source != store.UsageCapSourceAgentBudget {
		t.Fatalf("source = %q, want %q", source, store.UsageCapSourceAgentBudget)
	}
	if costMicros != wantCostMicros {
		t.Fatalf("max_cost_micros = %d, want %d", costMicros, wantCostMicros)
	}
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
