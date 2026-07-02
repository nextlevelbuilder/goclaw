package agent

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

func TestCalculateLLMCostUsesResolvedUsagePricingBeforeConfigFallback(t *testing.T) {
	tenantID := uuid.New()
	providerID := uuid.New()
	input := "0.000001"
	output := "0.000002"
	usageStore := &tracePricingUsageStore{resolved: &store.ResolvedUsagePricing{
		ModelID: "openai/gpt-4o-mini",
		Source:  "catalog",
		Pricing: store.UsagePricingFields{Input: &input, Output: &output},
	}}
	providerStore := &tracePricingProviderStore{provider: &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: providerID},
		TenantID:     tenantID,
		Name:         "openai",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "sk-test",
		Enabled:      true,
	}}
	loop := &Loop{
		tenantID:  tenantID,
		usageCaps: usagecaps.NewService(usageStore, providerStore),
		modelPricing: map[string]*config.ModelPricing{
			"openai/gpt-4o-mini": {InputPerMillion: 1000, OutputPerMillion: 1000},
		},
	}

	got := loop.calculateLLMCost(context.Background(), "openai", "gpt-4o-mini", &providers.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	if !floatClose(got, 0.002) {
		t.Fatalf("cost = %.9f, want 0.002", got)
	}
	if usageStore.resolveCalls != 1 {
		t.Fatalf("ResolvePricing calls = %d, want 1", usageStore.resolveCalls)
	}
	if usageStore.providerID != providerID {
		t.Fatalf("providerID = %s, want %s", usageStore.providerID, providerID)
	}
}

func TestCalculateLLMCostFallsBackToConfigWhenCatalogMissing(t *testing.T) {
	tenantID := uuid.New()
	usageStore := &tracePricingUsageStore{resolveErr: sql.ErrNoRows}
	providerStore := &tracePricingProviderStore{provider: &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "sk-test",
		Enabled:      true,
	}}
	loop := &Loop{
		tenantID:  tenantID,
		usageCaps: usagecaps.NewService(usageStore, providerStore),
		modelPricing: map[string]*config.ModelPricing{
			"openai/gpt-4o-mini": {InputPerMillion: 10, OutputPerMillion: 20},
		},
	}

	got := loop.calculateLLMCost(context.Background(), "openai", "gpt-4o-mini", &providers.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	if !floatClose(got, 0.02) {
		t.Fatalf("cost = %.9f, want 0.02", got)
	}
}

func floatClose(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

type tracePricingUsageStore struct {
	resolved     *store.ResolvedUsagePricing
	resolveErr   error
	resolveCalls int
	tenantID     uuid.UUID
	providerID   uuid.UUID
	providerName string
	providerType string
	modelID      string
}

func (s *tracePricingUsageStore) UpsertPricingCatalog(context.Context, []store.UsagePricingCatalogEntry) (int, error) {
	return 0, nil
}
func (s *tracePricingUsageStore) ListPricingCatalog(context.Context, store.UsagePricingQuery) ([]store.UsagePricingCatalogEntry, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) PutPricingOverride(context.Context, *store.UsagePricingOverride) error {
	return nil
}
func (s *tracePricingUsageStore) ListPricingOverrides(context.Context, store.UsagePricingQuery) ([]store.UsagePricingOverride, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) DeletePricingOverride(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *tracePricingUsageStore) ResolvePricing(_ context.Context, tenantID, providerID uuid.UUID, providerName, providerType, modelID string) (*store.ResolvedUsagePricing, error) {
	s.resolveCalls++
	s.tenantID = tenantID
	s.providerID = providerID
	s.providerName = providerName
	s.providerType = providerType
	s.modelID = modelID
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	return s.resolved, nil
}
func (s *tracePricingUsageStore) CreateUsageCapPolicy(context.Context, *store.UsageCapPolicy) error {
	return nil
}
func (s *tracePricingUsageStore) ListUsageCapPolicies(context.Context, store.UsageCapScope, bool) ([]store.UsageCapPolicy, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) UpdateUsageCapPolicy(context.Context, uuid.UUID, uuid.UUID, store.UsageCapPolicyPatch) (*store.UsageCapPolicy, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) DeleteUsageCapPolicy(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *tracePricingUsageStore) ReserveUsage(context.Context, store.UsageReserveRequest, []store.UsageCapPolicy) (*store.UsageReservationResult, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) ReconcileUsage(context.Context, store.UsageReconcileRequest) error {
	return nil
}
func (s *tracePricingUsageStore) ListUsageCapUtilization(context.Context, uuid.UUID) ([]store.UsageCapUtilization, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) ListUsageCapEvents(context.Context, uuid.UUID, int) ([]store.UsageCapEvent, error) {
	return nil, nil
}
func (s *tracePricingUsageStore) InsertUsageCapEvent(context.Context, *store.UsageCapEvent) error {
	return nil
}

type tracePricingProviderStore struct {
	provider *store.LLMProviderData
}

func (s *tracePricingProviderStore) CreateProvider(context.Context, *store.LLMProviderData) error {
	return nil
}
func (s *tracePricingProviderStore) GetProvider(context.Context, uuid.UUID) (*store.LLMProviderData, error) {
	if s.provider == nil {
		return nil, sql.ErrNoRows
	}
	return s.provider, nil
}
func (s *tracePricingProviderStore) GetProviderByName(context.Context, string) (*store.LLMProviderData, error) {
	if s.provider == nil {
		return nil, sql.ErrNoRows
	}
	return s.provider, nil
}
func (s *tracePricingProviderStore) ListProviders(context.Context) ([]store.LLMProviderData, error) {
	return nil, nil
}
func (s *tracePricingProviderStore) ListAllProviders(context.Context) ([]store.LLMProviderData, error) {
	return nil, nil
}
func (s *tracePricingProviderStore) UpdateProvider(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (s *tracePricingProviderStore) DeleteProvider(context.Context, uuid.UUID) error {
	return nil
}
