//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func int64Ptr(v int64) *int64 { return &v }

// seedSQLiteUsageCapTenant inserts a tenant + agent and registers cleanup.
func seedSQLiteUsageCapTenant(t *testing.T, db execer, tenantID, agentID uuid.UUID) {
	t.Helper()
	seedSQLiteRunTimelineTenant(t, db, tenantID)
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, ?, 'predefined', 'active', 'test', 'test-model', 'owner')`,
		agentID, tenantID, "usage-cap-agent-"+agentID.String(),
	); err != nil {
		t.Fatalf("seed usage cap agent: %v", err)
	}
}

// seedSQLiteUsageCapProvider inserts a provider owned by the tenant (so policy
// refs validate against tenant scope, master tenant, or both).
func seedSQLiteUsageCapProvider(t *testing.T, db execer, providerID, tenantID uuid.UUID, name, providerType string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO llm_providers (id, name, display_name, provider_type, api_base, api_key, enabled, settings, tenant_id)
		 VALUES (?, ?, ?, ?, ?, 'sk-test', 1, '{}', ?)`,
		providerID, name, name, providerType, "https://api.example.com/v1", tenantID,
	); err != nil {
		t.Fatalf("seed usage cap provider: %v", err)
	}
}

func TestSQLiteUsageCapPolicyCRUD(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	agentID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, agentID)

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()

	// Create a tenant-wide policy.
	maxTokens := int64(100_000)
	maxCost := int64(50_000_000) // $50
	warnAt := 80.0
	p := &store.UsageCapPolicy{
		TenantID: tenantID, Window: store.UsageCapWindowMonth,
		MaxTokens: &maxTokens, MaxCostMicros: &maxCost, WarnAtPercent: &warnAt,
		Enabled: true, Priority: 100,
	}
	if err := storeImpl.CreateUsageCapPolicy(ctx, p); err != nil {
		t.Fatalf("CreateUsageCapPolicy: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Fatal("CreateUsageCapPolicy did not assign ID")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Fatal("CreateUsageCapPolicy did not stamp timestamps")
	}

	// List includes the created policy (enabled scope).
	list, err := storeImpl.ListUsageCapPolicies(ctx, store.UsageCapScope{TenantID: tenantID}, false)
	if err != nil {
		t.Fatalf("ListUsageCapPolicies: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListUsageCapPolicies returned %d policies, want 1", len(list))
	}
	got := list[0]
	if got.ID != p.ID || got.MaxTokens == nil || *got.MaxTokens != maxTokens {
		t.Fatalf("listed policy mismatch: %+v", got)
	}
	if got.WarnAtPercent == nil || *got.WarnAtPercent != warnAt {
		t.Fatalf("warn_at_percent = %v, want %v", got.WarnAtPercent, warnAt)
	}

	// Update: raise the warn threshold and disable. WarnAtPercent is a
	// **float64 patch (nil = leave unchanged, &ptr = set to *ptr), so build the
	// inner pointer first then take its address.
	newWarnVal := 95.0
	newWarn := &newWarnVal
	disabled := false
	updated, err := storeImpl.UpdateUsageCapPolicy(ctx, tenantID, p.ID, store.UsageCapPolicyPatch{
		WarnAtPercent: &newWarn,
		Enabled:       &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateUsageCapPolicy: %v", err)
	}
	if updated.WarnAtPercent == nil || *updated.WarnAtPercent != newWarnVal {
		t.Fatalf("updated warn_at_percent = %v, want %v", updated.WarnAtPercent, newWarn)
	}
	if updated.Enabled {
		t.Fatal("updated policy still enabled, want disabled")
	}

	// Disabled policies are hidden from the enabled-only list.
	list, err = storeImpl.ListUsageCapPolicies(ctx, store.UsageCapScope{TenantID: tenantID}, false)
	if err != nil {
		t.Fatalf("ListUsageCapPolicies (enabled): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("enabled-only list returned %d policies, want 0", len(list))
	}
	list, err = storeImpl.ListUsageCapPolicies(ctx, store.UsageCapScope{TenantID: tenantID}, true)
	if err != nil {
		t.Fatalf("ListUsageCapPolicies (all): %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("all-policies list returned %d policies, want 1", len(list))
	}

	// Delete.
	if err := storeImpl.DeleteUsageCapPolicy(ctx, tenantID, p.ID); err != nil {
		t.Fatalf("DeleteUsageCapPolicy: %v", err)
	}
	// Updating a deleted policy must report no rows.
	if _, err := storeImpl.UpdateUsageCapPolicy(ctx, tenantID, p.ID, store.UsageCapPolicyPatch{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateUsageCapPolicy after delete error = %v, want sql.ErrNoRows", err)
	}
	list, err = storeImpl.ListUsageCapPolicies(ctx, store.UsageCapScope{TenantID: tenantID}, true)
	if err != nil {
		t.Fatalf("ListUsageCapPolicies after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("policies after delete = %d, want 0", len(list))
	}
}

func TestSQLiteUsageCapReconcileMovesReservedToUsed(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()
	maxTokens := int64(1_000)
	policy := &store.UsageCapPolicy{TenantID: tenantID, Window: store.UsageCapWindowDay, MaxTokens: &maxTokens, Enabled: true, Priority: 100}
	if err := storeImpl.CreateUsageCapPolicy(ctx, policy); err != nil {
		t.Fatalf("CreateUsageCapPolicy: %v", err)
	}

	// Reserve twice with the same reservation key — must be idempotent.
	req := store.UsageReserveRequest{
		UsageCapScope:   store.UsageCapScope{TenantID: tenantID},
		ReservationKey:  "reserve-idempotent",
		EstimatedTokens: 100,
	}
	for i := 0; i < 2; i++ {
		if _, err := storeImpl.ReserveUsage(ctx, req, []store.UsageCapPolicy{*policy}); err != nil {
			t.Fatalf("ReserveUsage call %d: %v", i+1, err)
		}
	}

	util, err := storeImpl.ListUsageCapUtilization(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListUsageCapUtilization: %v", err)
	}
	if len(util) != 1 || util[0].ReservedTokens != 100 {
		t.Fatalf("reserved_tokens = %d, want 100", util[0].ReservedTokens)
	}

	// Reconcile twice concurrently is safe; used lands at 7 once.
	if err := storeImpl.ReconcileUsage(ctx, store.UsageReconcileRequest{
		ReservationKey: req.ReservationKey,
		ActualTokens:   7,
		Status:         "reconciled",
	}); err != nil {
		t.Fatalf("ReconcileUsage: %v", err)
	}
	if err := storeImpl.ReconcileUsage(ctx, store.UsageReconcileRequest{
		ReservationKey: req.ReservationKey,
		ActualTokens:   7,
		Status:         "reconciled",
	}); err != nil {
		t.Fatalf("ReconcileUsage (2nd): %v", err)
	}

	util, err = storeImpl.ListUsageCapUtilization(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListUsageCapUtilization (post): %v", err)
	}
	if len(util) != 1 {
		t.Fatalf("utilization rows = %d, want 1", len(util))
	}
	if util[0].UsedTokens != 7 {
		t.Fatalf("used_tokens = %d, want 7", util[0].UsedTokens)
	}
	if util[0].ReservedTokens != 93 {
		t.Fatalf("reserved_tokens = %d, want 93 (100 reserved - 7 used)", util[0].ReservedTokens)
	}
}

func TestSQLiteUsageCapReserveRejectsOverBudget(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()
	maxTokens := int64(10)
	policy := &store.UsageCapPolicy{TenantID: tenantID, Window: store.UsageCapWindowHour, MaxTokens: &maxTokens, Enabled: true, Priority: 100}
	if err := storeImpl.CreateUsageCapPolicy(ctx, policy); err != nil {
		t.Fatalf("CreateUsageCapPolicy: %v", err)
	}

	// First reserve fits (6 <= 10).
	if _, err := storeImpl.ReserveUsage(ctx, store.UsageReserveRequest{
		UsageCapScope: store.UsageCapScope{TenantID: tenantID}, ReservationKey: "fit", EstimatedTokens: 6,
	}, []store.UsageCapPolicy{*policy}); err != nil {
		t.Fatalf("ReserveUsage (fit): %v", err)
	}
	// Second reserve would push 6+6=12 > 10.
	_, err := storeImpl.ReserveUsage(ctx, store.UsageReserveRequest{
		UsageCapScope: store.UsageCapScope{TenantID: tenantID}, ReservationKey: "exceed", EstimatedTokens: 6,
	}, []store.UsageCapPolicy{*policy})
	if !errors.Is(err, store.ErrUsageCapExceeded) {
		t.Fatalf("ReserveUsage (exceed) error = %v, want ErrUsageCapExceeded", err)
	}
}

func TestSQLiteUsageCapGetBudgetUsageMath(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()
	maxTokens := int64(1_000)
	maxCost := int64(1_000) // micros
	policy := &store.UsageCapPolicy{
		TenantID: tenantID, Window: store.UsageCapWindowDay,
		MaxTokens: &maxTokens, MaxCostMicros: &maxCost, Enabled: true, Priority: 100,
	}
	if err := storeImpl.CreateUsageCapPolicy(ctx, policy); err != nil {
		t.Fatalf("CreateUsageCapPolicy: %v", err)
	}

	// Reserve 300 cost, reconcile to 200 actual. 100 stays reserved.
	if _, err := storeImpl.ReserveUsage(ctx, store.UsageReserveRequest{
		UsageCapScope: store.UsageCapScope{TenantID: tenantID}, ReservationKey: "math",
		EstimatedTokens: 400, EstimatedCostMicros: 300,
	}, []store.UsageCapPolicy{*policy}); err != nil {
		t.Fatalf("ReserveUsage: %v", err)
	}
	if err := storeImpl.ReconcileUsage(ctx, store.UsageReconcileRequest{
		ReservationKey: "math", ActualTokens: 250, ActualCostMicros: 200, Status: "reconciled",
	}); err != nil {
		t.Fatalf("ReconcileUsage: %v", err)
	}

	// Pin the window to the same bounds ReserveUsage/ReconcileUsage used, so
	// the assertion is immune to a UTC-day boundary crossing mid-test.
	start, end := usageWindow(time.Now().UTC(), store.UsageCapWindowDay)
	rows, err := storeImpl.GetBudgetUsage(ctx, tenantID, store.BudgetUsageWindow{Start: start, End: end})
	if err != nil {
		t.Fatalf("GetBudgetUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("GetBudgetUsage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.UsedTokens != 250 {
		t.Fatalf("used_tokens = %d, want 250", row.UsedTokens)
	}
	if row.ReservedTokens != 150 {
		t.Fatalf("reserved_tokens = %d, want 150", row.ReservedTokens)
	}
	if row.UsedCostMicros != 200 {
		t.Fatalf("used_cost_micros = %d, want 200", row.UsedCostMicros)
	}
	if row.ReservedCostMicros != 100 {
		t.Fatalf("reserved_cost_micros = %d, want 100", row.ReservedCostMicros)
	}
	// Cost limit wins when both are set: (200+100)/1000 = 0.3.
	if math.Abs(row.PercentUsed-0.3) > 1e-9 {
		t.Fatalf("percent_used = %v, want 0.3", row.PercentUsed)
	}

	// A window override selects the counters for that window; a fresh window
	// yields a zero row (no counter yet) rather than an error.
	freshStart := row.WindowEnd
	rows, err = storeImpl.GetBudgetUsage(ctx, tenantID, store.BudgetUsageWindow{Start: freshStart, End: freshStart.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("GetBudgetUsage (fresh window): %v", err)
	}
	if len(rows) != 1 || rows[0].UsedTokens != 0 || rows[0].UsedCostMicros != 0 {
		t.Fatalf("fresh-window row = %+v, want zero usage", rows[0])
	}
}

func TestSQLiteUsageCapWarnDedupPerWindow(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()
	warnAt := 50.0
	policy := &store.UsageCapPolicy{
		TenantID: tenantID, Window: store.UsageCapWindowDay,
		MaxTokens: int64Ptr(1000), WarnAtPercent: &warnAt, Enabled: true, Priority: 100,
	}
	if err := storeImpl.CreateUsageCapPolicy(ctx, policy); err != nil {
		t.Fatalf("CreateUsageCapPolicy: %v", err)
	}

	start, _ := usageWindow(time.Now().UTC(), store.UsageCapWindowDay)

	// Before any warn event, the window is not warned.
	fired, err := storeImpl.BudgetWindowWarned(ctx, tenantID, policy.ID, start)
	if err != nil {
		t.Fatalf("BudgetWindowWarned: %v", err)
	}
	if fired {
		t.Fatal("BudgetWindowWarned = true before any warn event")
	}

	// Insert the warn event the reconcile path produces (decision='warn',
	// metadata.window_start in RFC3339).
	warnEvent := &store.UsageCapEvent{
		TenantID:   tenantID,
		PolicyID:   &policy.ID,
		Decision:   store.UsageCapEventWarn,
		Reason:     "budget_threshold",
		ActualTokens: 600,
		Metadata:   json.RawMessage(`{"window_start":"` + start.UTC().Format(time.RFC3339) + `","reason_group":"goclaw.budget"}`),
	}
	if err := storeImpl.InsertUsageCapEvent(ctx, warnEvent); err != nil {
		t.Fatalf("InsertUsageCapEvent: %v", err)
	}

	fired, err = storeImpl.BudgetWindowWarned(ctx, tenantID, policy.ID, start)
	if err != nil {
		t.Fatalf("BudgetWindowWarned (post): %v", err)
	}
	if !fired {
		t.Fatal("BudgetWindowWarned = false after warn event for same window")
	}

	// A different window is not considered warned.
	nextStart := start.Add(24 * time.Hour)
	fired, err = storeImpl.BudgetWindowWarned(ctx, tenantID, policy.ID, nextStart)
	if err != nil {
		t.Fatalf("BudgetWindowWarned (next window): %v", err)
	}
	if fired {
		t.Fatal("BudgetWindowWarned = true for a window that never warned")
	}

	// GetBudgetUsage surfaces Warned=true for the warned window.
	rows, err := storeImpl.GetBudgetUsage(ctx, tenantID, store.BudgetUsageWindow{Start: start, End: start.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("GetBudgetUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("GetBudgetUsage rows = %d, want 1", len(rows))
	}
	if !rows[0].Warned {
		t.Fatal("GetBudgetUsage Warned = false, want true")
	}
}

func TestSQLiteUsageCapPricingResolve(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))
	providerID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapProvider(t, db, providerID, tenantID, "openrouter", store.ProviderOpenRouter)

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()

	// Seed the catalog with an unqualified model; resolve should expand the
	// provider/model prefix (openai/gpt-4o).
	input := "0.01"
	output := "0.03"
	count, err := storeImpl.UpsertPricingCatalog(ctx, []store.UsagePricingCatalogEntry{{
		ModelID:  "gpt-4o",
		Pricing:  store.UsagePricingFields{Input: &input, Output: &output},
		SyncedAt: time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("UpsertPricingCatalog: %v", err)
	}
	if count != 1 {
		t.Fatalf("UpsertPricingCatalog count = %d, want 1", count)
	}

	resolved, err := storeImpl.ResolvePricing(ctx, tenantID, providerID, "openrouter", store.ProviderOpenRouter, "gpt-4o")
	if err != nil {
		t.Fatalf("ResolvePricing (catalog): %v", err)
	}
	if resolved.Source != "catalog" {
		t.Fatalf("ResolvePricing source = %q, want catalog", resolved.Source)
	}
	if resolved.Pricing.Input == nil || *resolved.Pricing.Input != input {
		t.Fatalf("ResolvePricing input = %v, want %q", resolved.Pricing.Input, input)
	}

	// A tenant override beats the catalog for the same model.
	overrideInput := "0.02"
	if err := storeImpl.PutPricingOverride(ctx, &store.UsagePricingOverride{
		TenantID: tenantID, ProviderID: providerID, ProviderType: store.ProviderOpenRouter,
		ModelID: "gpt-4o", Pricing: store.UsagePricingFields{Input: &overrideInput, Output: &output},
		Enabled: true,
	}); err != nil {
		t.Fatalf("PutPricingOverride: %v", err)
	}
	resolved, err = storeImpl.ResolvePricing(ctx, tenantID, providerID, "openrouter", store.ProviderOpenRouter, "gpt-4o")
	if err != nil {
		t.Fatalf("ResolvePricing (override): %v", err)
	}
	if resolved.Source != "override" {
		t.Fatalf("ResolvePricing source = %q, want override", resolved.Source)
	}
	if resolved.Pricing.Input == nil || *resolved.Pricing.Input != overrideInput {
		t.Fatalf("override input = %v, want %q", resolved.Pricing.Input, overrideInput)
	}

	// Unknown model → ErrNoRows.
	if _, err := storeImpl.ResolvePricing(ctx, tenantID, providerID, "openrouter", store.ProviderOpenRouter, "definitely/missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ResolvePricing (missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestSQLiteUsageCapRejectsCrossTenantAgent(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	agentB := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantA, uuid.Must(uuid.NewV7()))
	seedSQLiteUsageCapTenant(t, db, tenantB, agentB)

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()
	p := &store.UsageCapPolicy{
		TenantID: tenantA, AgentID: &agentB, Window: store.UsageCapWindowDay,
		MaxTokens: int64Ptr(100), Enabled: true, Priority: 100,
	}
	if err := storeImpl.CreateUsageCapPolicy(ctx, p); err == nil {
		t.Fatal("CreateUsageCapPolicy accepted agent from another tenant")
	}
}

func TestSQLiteUsageCapEventsListNewestFirst(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteUsageCapTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	storeImpl := NewSQLiteUsageCapStore(db)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		e := &store.UsageCapEvent{TenantID: tenantID, Decision: store.UsageCapEventAllow, Reason: "test"}
		if err := storeImpl.InsertUsageCapEvent(ctx, e); err != nil {
			t.Fatalf("InsertUsageCapEvent %d: %v", i+1, err)
		}
	}
	events, err := storeImpl.ListUsageCapEvents(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("ListUsageCapEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	for i := 1; i < len(events); i++ {
		// Newest-first: each earlier element must be >= the next. An older item
		// appearing before a newer one violates the descending order.
		if events[i-1].CreatedAt.Before(events[i].CreatedAt) {
			t.Fatalf("events not newest-first: %v then %v", events[i-1].CreatedAt, events[i].CreatedAt)
		}
	}
}
