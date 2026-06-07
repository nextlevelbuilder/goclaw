package pg

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestPGUsageCapStoreReserveUsageIdempotent(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	usageStore := NewPGUsageCapStore(db)
	maxTokens := int64(100)
	policy := &store.UsageCapPolicy{
		TenantID: tenantID, Window: store.UsageCapWindowHour,
		MaxTokens: &maxTokens, Enabled: true, Priority: 100,
	}
	if err := usageStore.CreateUsageCapPolicy(context.Background(), policy); err != nil {
		t.Fatalf("CreateUsageCapPolicy: %v", err)
	}
	req := store.UsageReserveRequest{
		UsageCapScope:   store.UsageCapScope{TenantID: tenantID},
		ReservationKey:  "duplicate-reservation",
		EstimatedTokens: 10,
	}
	for i := range 2 {
		if _, err := usageStore.ReserveUsage(context.Background(), req, []store.UsageCapPolicy{*policy}); err != nil {
			t.Fatalf("ReserveUsage call %d: %v", i+1, err)
		}
	}

	var reservedTokens, reservationRows int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(reserved_tokens),0) FROM usage_cap_counters WHERE policy_id=$1`, policy.ID).Scan(&reservedTokens); err != nil {
		t.Fatalf("query reserved tokens: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_cap_reservations WHERE policy_id=$1 AND reservation_key=$2`, policy.ID, req.ReservationKey).Scan(&reservationRows); err != nil {
		t.Fatalf("query reservation rows: %v", err)
	}
	if reservedTokens != 10 {
		t.Fatalf("reserved_tokens = %d, want 10", reservedTokens)
	}
	if reservationRows != 1 {
		t.Fatalf("reservation rows = %d, want 1", reservationRows)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- usageStore.ReconcileUsage(context.Background(), store.UsageReconcileRequest{
				ReservationKey: req.ReservationKey,
				ActualTokens:   7,
				Status:         "reconciled",
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ReconcileUsage: %v", err)
		}
	}
	var usedTokens int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(used_tokens),0) FROM usage_cap_counters WHERE policy_id=$1`, policy.ID).Scan(&usedTokens); err != nil {
		t.Fatalf("query used tokens: %v", err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(reserved_tokens),0) FROM usage_cap_counters WHERE policy_id=$1`, policy.ID).Scan(&reservedTokens); err != nil {
		t.Fatalf("query reserved tokens after reconcile: %v", err)
	}
	if usedTokens != 7 {
		t.Fatalf("used_tokens = %d, want 7", usedTokens)
	}
	if reservedTokens != 0 {
		t.Fatalf("reserved_tokens after reconcile = %d, want 0", reservedTokens)
	}
}

func TestPGUsageCapStoreRejectsCrossTenantRefs(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, agentA := seedTenantAndAgent(t, db)
	tenantB, _ := seedTenantAndAgent(t, db)
	usageStore := NewPGUsageCapStore(db)

	policy := &store.UsageCapPolicy{
		TenantID: tenantB, AgentID: &agentA, Window: store.UsageCapWindowDay,
		MaxTokens: int64PtrPG(100), Enabled: true,
	}
	if err := usageStore.CreateUsageCapPolicy(context.Background(), policy); err == nil {
		t.Fatal("CreateUsageCapPolicy accepted agent_id from another tenant")
	}

	providerID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO llm_providers (id, tenant_id, name, provider_type, api_key, enabled)
		 VALUES ($1,$2,$3,'openrouter','sk-test',true)`,
		providerID, tenantA, "ucp-"+providerID.String(),
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	override := &store.UsagePricingOverride{
		TenantID: tenantB, ProviderID: providerID, ProviderType: store.ProviderOpenRouter,
		ModelID: "openai/gpt-test", Enabled: true,
	}
	if err := usageStore.PutPricingOverride(context.Background(), override); err == nil {
		t.Fatal("PutPricingOverride accepted provider_id from another tenant")
	}

	masterProviderID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO llm_providers (id, tenant_id, name, provider_type, api_key, enabled)
		 VALUES ($1,$2,$3,'openrouter','sk-test',true)`,
		masterProviderID, store.MasterTenantID, "ucpm-"+masterProviderID.String(),
	); err != nil {
		t.Fatalf("seed master provider: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM llm_providers WHERE id=$1", masterProviderID)
	})
	masterScopedPolicy := &store.UsageCapPolicy{
		TenantID: tenantB, ProviderID: &masterProviderID, Window: store.UsageCapWindowDay,
		MaxTokens: int64PtrPG(100), Enabled: true,
	}
	if err := usageStore.CreateUsageCapPolicy(context.Background(), masterScopedPolicy); err != nil {
		t.Fatalf("CreateUsageCapPolicy rejected master provider ref: %v", err)
	}
}

func TestValidateUsagePricingFieldsRejectsNegativeValues(t *testing.T) {
	negative := "-0.01"
	if err := validateUsagePricingFields(store.UsagePricingFields{Input: &negative}); err == nil {
		t.Fatal("validateUsagePricingFields accepted negative price")
	}
}

func TestPGUsageCapStoreResolvePricingUsesOpenRouterAliases(t *testing.T) {
	db := hooksTestDB(t)
	usageStore := NewPGUsageCapStore(db)
	inputPrice := "0.000001"
	outputPrice := "0.000002"
	entries := []store.UsagePricingCatalogEntry{
		{ModelID: "openai/gpt-4o-mini", CanonicalModelID: "openai/gpt-4o-mini", Pricing: store.UsagePricingFields{Input: &inputPrice, Output: &outputPrice}, SyncedAt: time.Now().UTC()},
		{ModelID: "anthropic/claude-3-5-haiku-latest", CanonicalModelID: "anthropic/claude-3-5-haiku-latest", Pricing: store.UsagePricingFields{Input: &inputPrice, Output: &outputPrice}, SyncedAt: time.Now().UTC()},
		{ModelID: "google/gemini-2.5-flash", CanonicalModelID: "google/gemini-2.5-flash", Pricing: store.UsagePricingFields{Input: &inputPrice, Output: &outputPrice}, SyncedAt: time.Now().UTC()},
	}
	if _, err := usageStore.UpsertPricingCatalog(context.Background(), entries); err != nil {
		t.Fatalf("UpsertPricingCatalog: %v", err)
	}

	cases := []struct {
		name         string
		providerName string
		providerType string
		modelID      string
		wantModelID  string
	}{
		{name: "openai compat", providerName: "openai", providerType: store.ProviderOpenAICompat, modelID: "gpt-4o-mini", wantModelID: "openai/gpt-4o-mini"},
		{name: "anthropic native", providerName: "anthropic", providerType: store.ProviderAnthropicNative, modelID: "claude-3-5-haiku-latest", wantModelID: "anthropic/claude-3-5-haiku-latest"},
		{name: "gemini native", providerName: "gemini", providerType: store.ProviderGeminiNative, modelID: "gemini-2.5-flash", wantModelID: "google/gemini-2.5-flash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := usageStore.ResolvePricing(context.Background(), uuid.New(), uuid.New(), tc.providerName, tc.providerType, tc.modelID)
			if err != nil {
				t.Fatalf("ResolvePricing: %v", err)
			}
			if resolved.ModelID != tc.wantModelID {
				t.Fatalf("resolved model = %q, want %q", resolved.ModelID, tc.wantModelID)
			}
		})
	}
}

func int64PtrPG(v int64) *int64 { return &v }
