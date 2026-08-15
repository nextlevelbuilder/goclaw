package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	UsageCapWindowHour  = "hour"
	UsageCapWindowDay   = "day"
	UsageCapWindowWeek  = "week"
	UsageCapWindowMonth = "month"

	UsageCapEventAllow     = "allow"
	UsageCapEventBlock     = "block"
	UsageCapEventReconcile = "reconcile"
	UsageCapEventSkip      = "skip"

	UsageCapSourceManual      = "manual"
	UsageCapSourceAgentBudget = "agent_budget_monthly_cents"
)

var (
	ErrUsageCapExceeded      = errors.New("usage cap exceeded")
	ErrUsageCapPolicyManaged = errors.New("usage cap policy is managed by another setting")
)

type UsageCapExceededError struct {
	PolicyID uuid.UUID
	Reason   string
}

func (e *UsageCapExceededError) Error() string {
	return ErrUsageCapExceeded.Error()
}

func (e *UsageCapExceededError) Unwrap() error {
	return ErrUsageCapExceeded
}

// UsagePricingFields stores OpenRouter-compatible USD prices as decimal strings.
// Nil means unknown; "0" means explicitly free.
type UsagePricingFields struct {
	Input      *string `json:"input,omitempty"`
	Output     *string `json:"output,omitempty"`
	CacheRead  *string `json:"cache_read,omitempty"`
	CacheWrite *string `json:"cache_write,omitempty"`
	Reasoning  *string `json:"reasoning,omitempty"`
	Request    *string `json:"request,omitempty"`
	Image      *string `json:"image,omitempty"`
	WebSearch  *string `json:"web_search,omitempty"`
}

type UsagePricingCatalogEntry struct {
	ID               uuid.UUID          `json:"id" db:"id"`
	ModelID          string             `json:"model_id" db:"model_id"`
	CanonicalModelID string             `json:"canonical_model_id,omitempty" db:"canonical_model_id"`
	Pricing          UsagePricingFields `json:"pricing"`
	RawPricing       json.RawMessage    `json:"raw_pricing,omitempty" db:"raw_pricing"`
	RawModel         json.RawMessage    `json:"raw_model,omitempty" db:"raw_model"`
	SyncedAt         time.Time          `json:"synced_at" db:"synced_at"`
	CreatedAt        time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at" db:"updated_at"`
}

type UsagePricingOverride struct {
	ID           uuid.UUID          `json:"id" db:"id"`
	TenantID     uuid.UUID          `json:"tenant_id" db:"tenant_id"`
	ProviderID   uuid.UUID          `json:"provider_id" db:"provider_id"`
	ProviderType string             `json:"provider_type" db:"provider_type"`
	ModelID      string             `json:"model_id" db:"model_id"`
	Pricing      UsagePricingFields `json:"pricing"`
	Enabled      bool               `json:"enabled" db:"enabled"`
	CreatedAt    time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at" db:"updated_at"`
}

type UsagePricingQuery struct {
	TenantID     uuid.UUID
	ProviderID   uuid.UUID
	ProviderType string
	ModelID      string
	Limit        int
}

type ResolvedUsagePricing struct {
	ModelID       string             `json:"model_id"`
	ProviderID    uuid.UUID          `json:"provider_id,omitempty"`
	ProviderType  string             `json:"provider_type,omitempty"`
	Source        string             `json:"source"`
	Pricing       UsagePricingFields `json:"pricing"`
	CatalogSynced *time.Time         `json:"catalog_synced_at,omitempty"`
	OverrideID    uuid.UUID          `json:"override_id,omitempty"`
}

type UsageCapPolicy struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	TenantID      uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AgentID       *uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	ProviderID    *uuid.UUID `json:"provider_id,omitempty" db:"provider_id"`
	ProviderType  string     `json:"provider_type,omitempty" db:"provider_type"`
	ModelID       string     `json:"model_id,omitempty" db:"model_id"`
	Window        string     `json:"window" db:"window"`
	MaxTokens     *int64     `json:"max_tokens,omitempty" db:"max_tokens"`
	MaxCostMicros *int64     `json:"max_cost_micros,omitempty" db:"max_cost_micros"`
	Source        string     `json:"source,omitempty" db:"source"`
	Enabled       bool       `json:"enabled" db:"enabled"`
	Priority      int        `json:"priority" db:"priority"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

type UsageCapPolicyPatch struct {
	AgentID       **uuid.UUID
	ProviderID    **uuid.UUID
	ProviderType  *string
	ModelID       *string
	Window        *string
	MaxTokens     **int64
	MaxCostMicros **int64
	Enabled       *bool
	Priority      *int
}

type UsageCapScope struct {
	TenantID     uuid.UUID
	AgentID      uuid.UUID
	ProviderID   uuid.UUID
	ProviderType string
	ModelID      string
}

type UsageReserveRequest struct {
	UsageCapScope
	ReservationKey      string
	EstimatedTokens     int64
	EstimatedCostMicros int64
	Metadata            json.RawMessage
}

type UsageReservationResult struct {
	ReservationKey string           `json:"reservation_key"`
	Policies       []UsageCapPolicy `json:"policies"`
	Skipped        bool             `json:"skipped,omitempty"`
	Reason         string           `json:"reason,omitempty"`
}

type UsageReconcileRequest struct {
	ReservationKey   string
	ActualTokens     int64
	ActualCostMicros int64
	Status           string
	Metadata         json.RawMessage
}

type UsageCapUtilization struct {
	Policy             UsageCapPolicy `json:"policy"`
	WindowStart        time.Time      `json:"window_start"`
	WindowEnd          time.Time      `json:"window_end"`
	UsedTokens         int64          `json:"used_tokens"`
	ReservedTokens     int64          `json:"reserved_tokens"`
	UsedCostMicros     int64          `json:"used_cost_micros"`
	ReservedCostMicros int64          `json:"reserved_cost_micros"`
}

type UsageCapEvent struct {
	ID                  uuid.UUID       `json:"id" db:"id"`
	TenantID            uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	PolicyID            *uuid.UUID      `json:"policy_id,omitempty" db:"policy_id"`
	ReservationKey      string          `json:"reservation_key,omitempty" db:"reservation_key"`
	Decision            string          `json:"decision" db:"decision"`
	Reason              string          `json:"reason,omitempty" db:"reason"`
	EstimatedTokens     int64           `json:"estimated_tokens" db:"estimated_tokens"`
	EstimatedCostMicros int64           `json:"estimated_cost_micros" db:"estimated_cost_micros"`
	ActualTokens        int64           `json:"actual_tokens" db:"actual_tokens"`
	ActualCostMicros    int64           `json:"actual_cost_micros" db:"actual_cost_micros"`
	Metadata            json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`
}

type UsageCapStore interface {
	UpsertPricingCatalog(ctx context.Context, entries []UsagePricingCatalogEntry) (int, error)
	ListPricingCatalog(ctx context.Context, q UsagePricingQuery) ([]UsagePricingCatalogEntry, error)
	PutPricingOverride(ctx context.Context, o *UsagePricingOverride) error
	ListPricingOverrides(ctx context.Context, q UsagePricingQuery) ([]UsagePricingOverride, error)
	DeletePricingOverride(ctx context.Context, tenantID, id uuid.UUID) error
	ResolvePricing(ctx context.Context, tenantID, providerID uuid.UUID, providerName, providerType, modelID string) (*ResolvedUsagePricing, error)

	CreateUsageCapPolicy(ctx context.Context, p *UsageCapPolicy) error
	ListUsageCapPolicies(ctx context.Context, scope UsageCapScope, includeDisabled bool) ([]UsageCapPolicy, error)
	UpdateUsageCapPolicy(ctx context.Context, tenantID, id uuid.UUID, patch UsageCapPolicyPatch) (*UsageCapPolicy, error)
	DeleteUsageCapPolicy(ctx context.Context, tenantID, id uuid.UUID) error
	ReserveUsage(ctx context.Context, req UsageReserveRequest, policies []UsageCapPolicy) (*UsageReservationResult, error)
	ReconcileUsage(ctx context.Context, req UsageReconcileRequest) error
	ListUsageCapUtilization(ctx context.Context, tenantID uuid.UUID) ([]UsageCapUtilization, error)
	ListUsageCapEvents(ctx context.Context, tenantID uuid.UUID, limit int) ([]UsageCapEvent, error)
	InsertUsageCapEvent(ctx context.Context, event *UsageCapEvent) error
}
