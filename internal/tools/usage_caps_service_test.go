package tools

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func int64Ptr(v int64) *int64 { return &v }

// fakeToolUsageCapStore is the minimum of store.UsageCapStore a Preflight needs:
// the policies in force, and a ReserveUsage that enforces their token cap.
type fakeToolUsageCapStore struct {
	policies         []store.UsageCapPolicy
	enforceTokenCaps bool
	reserved         store.UsageReserveRequest
	events           []store.UsageCapEvent
}

func (s *fakeToolUsageCapStore) UpsertPricingCatalog(context.Context, []store.UsagePricingCatalogEntry) (int, error) {
	return 0, nil
}

func (s *fakeToolUsageCapStore) ListPricingCatalog(context.Context, store.UsagePricingQuery) ([]store.UsagePricingCatalogEntry, error) {
	return nil, nil
}

func (s *fakeToolUsageCapStore) PutPricingOverride(context.Context, *store.UsagePricingOverride) error {
	return nil
}

func (s *fakeToolUsageCapStore) ListPricingOverrides(context.Context, store.UsagePricingQuery) ([]store.UsagePricingOverride, error) {
	return nil, nil
}

func (s *fakeToolUsageCapStore) DeletePricingOverride(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *fakeToolUsageCapStore) ResolvePricing(context.Context, uuid.UUID, uuid.UUID, string, string, string) (*store.ResolvedUsagePricing, error) {
	return nil, sql.ErrNoRows
}

func (s *fakeToolUsageCapStore) CreateUsageCapPolicy(context.Context, *store.UsageCapPolicy) error {
	return nil
}

func (s *fakeToolUsageCapStore) ListUsageCapPolicies(context.Context, store.UsageCapScope, bool) ([]store.UsageCapPolicy, error) {
	return s.policies, nil
}

func (s *fakeToolUsageCapStore) UpdateUsageCapPolicy(context.Context, uuid.UUID, uuid.UUID, store.UsageCapPolicyPatch) (*store.UsageCapPolicy, error) {
	return nil, nil
}

func (s *fakeToolUsageCapStore) DeleteUsageCapPolicy(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *fakeToolUsageCapStore) ReserveUsage(_ context.Context, req store.UsageReserveRequest, policies []store.UsageCapPolicy) (*store.UsageReservationResult, error) {
	s.reserved = req
	if s.enforceTokenCaps {
		for _, p := range policies {
			if p.MaxTokens != nil && req.EstimatedTokens > *p.MaxTokens {
				return nil, &store.UsageCapExceededError{PolicyID: p.ID, Reason: "max_tokens"}
			}
		}
	}
	return &store.UsageReservationResult{ReservationKey: req.ReservationKey, Policies: policies}, nil
}

func (s *fakeToolUsageCapStore) ReconcileUsage(context.Context, store.UsageReconcileRequest) error {
	return nil
}

func (s *fakeToolUsageCapStore) ListUsageCapUtilization(context.Context, uuid.UUID) ([]store.UsageCapUtilization, error) {
	return nil, nil
}

func (s *fakeToolUsageCapStore) ListUsageCapEvents(context.Context, uuid.UUID, int) ([]store.UsageCapEvent, error) {
	return nil, nil
}

func (s *fakeToolUsageCapStore) InsertUsageCapEvent(_ context.Context, event *store.UsageCapEvent) error {
	if event != nil {
		s.events = append(s.events, *event)
	}
	return nil
}

// fakeToolProviderStore serves the provider metadata Preflight scopes a
// reservation by. Only the lookup by name is ever reached.
type fakeToolProviderStore struct {
	provider *store.LLMProviderData
}

func (s *fakeToolProviderStore) CreateProvider(context.Context, *store.LLMProviderData) error {
	return nil
}

func (s *fakeToolProviderStore) GetProvider(context.Context, uuid.UUID) (*store.LLMProviderData, error) {
	return s.provider, nil
}

func (s *fakeToolProviderStore) GetProviderByName(context.Context, string) (*store.LLMProviderData, error) {
	if s.provider == nil {
		return nil, sql.ErrNoRows
	}
	return s.provider, nil
}

func (s *fakeToolProviderStore) ListProviders(context.Context) ([]store.LLMProviderData, error) {
	return nil, nil
}

func (s *fakeToolProviderStore) ListAllProviders(context.Context) ([]store.LLMProviderData, error) {
	return nil, nil
}

func (s *fakeToolProviderStore) UpdateProvider(context.Context, uuid.UUID, map[string]any) error {
	return nil
}

func (s *fakeToolProviderStore) DeleteProvider(context.Context, uuid.UUID) error { return nil }
