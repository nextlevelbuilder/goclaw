package caps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/usage/pricing"
)

var (
	ErrCapExceeded    = errors.New("usage cap exceeded")
	ErrPricingUnknown = pricing.ErrUnknownPricing
)

type Service struct {
	store     store.UsageCapStore
	providers store.ProviderStore
}

func NewService(s store.UsageCapStore, providers store.ProviderStore) *Service {
	if s == nil {
		return nil
	}
	return &Service{store: s, providers: providers}
}

type Request struct {
	TenantID        uuid.UUID
	AgentID         uuid.UUID
	ProviderName    string
	ModelID         string
	ReservationKey  string
	Messages        []providers.Message
	MaxOutputTokens int
}

type Reservation struct {
	key                 string
	result              *store.UsageReservationResult
	svc                 *Service
	usage               pricing.BillableUsage
	prices              store.UsagePricingFields
	estimatedCostMicros int64
	actualTokens        int64
	actualCostMicros    int64
	reconcileStatus     string
	skipped             bool
	decision            string
	reason              string
	blockedPolicyID     uuid.UUID
	providerName        string
	providerType        string
	modelID             string
}

func (s *Service) Preflight(ctx context.Context, req Request) (*Reservation, error) {
	if s == nil || s.store == nil {
		return skippedReservation(req, "service_disabled"), nil
	}
	ctx = scopedRequestContext(ctx, req)
	providerData, err := s.resolveProvider(ctx, req.TenantID, req.ProviderName)
	if err != nil {
		return skippedReservation(req, "provider_metadata_missing"), nil
	}
	scope := store.UsageCapScope{
		TenantID: req.TenantID, AgentID: req.AgentID, ProviderID: providerData.ID,
		ProviderType: providerData.ProviderType, ModelID: req.ModelID,
	}
	if !ShouldEnforceProvider(providerData.ProviderType, providerData.APIKey != "") {
		_ = s.store.InsertUsageCapEvent(ctx, &store.UsageCapEvent{
			TenantID: req.TenantID, Decision: store.UsageCapEventSkip,
			Reason: "provider_not_billable_api", Metadata: mustJSON(scope),
		})
		return skippedScopedReservation(req, scope, "provider_not_billable_api"), nil
	}
	policies, err := s.store.ListUsageCapPolicies(ctx, scope, false)
	if err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return skippedScopedReservation(req, scope, "no_policy"), nil
	}
	usage := pricing.BillableUsage{
		InputTokens:  int64(EstimateInputTokens(req.Messages)),
		OutputTokens: int64(req.MaxOutputTokens),
		ImageCount:   int64(CountImages(req.Messages)),
	}
	if usage.OutputTokens <= 0 {
		usage.OutputTokens = 1
	}
	key := req.ReservationKey
	if key == "" {
		key = uuid.NewString()
	}
	metadata := map[string]any{"model_id": req.ModelID, "pricing_source": "token_only"}
	var prices store.UsagePricingFields
	var costMicros int64
	if requiresCostCap(policies) {
		resolved, err := s.store.ResolvePricing(ctx, req.TenantID, providerData.ID, providerData.Name, providerData.ProviderType, req.ModelID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_ = s.store.InsertUsageCapEvent(ctx, &store.UsageCapEvent{
					TenantID:       req.TenantID,
					ReservationKey: key, Decision: store.UsageCapEventBlock, Reason: "pricing_unknown",
					EstimatedTokens: usage.TotalTokens(), EstimatedCostMicros: 0,
					Metadata: mustJSON(map[string]any{"model_id": req.ModelID, "provider": req.ProviderName}),
				})
				return blockedReservation(req, scope, key, usage, 0, uuid.Nil, "pricing_unknown"), fmt.Errorf("%w: %s", ErrPricingUnknown, req.ModelID)
			}
			return nil, err
		}
		prices = resolved.Pricing
		if prices.Request != nil {
			usage.RequestCount = 1
		}
		costMicros, err = pricing.CostMicros(prices, usage)
		if err != nil {
			return nil, err
		}
		metadata = map[string]any{"source": resolved.Source, "model_id": resolved.ModelID}
	}
	result, err := s.store.ReserveUsage(ctx, store.UsageReserveRequest{
		UsageCapScope: scope, ReservationKey: key,
		EstimatedTokens: usage.TotalTokens(), EstimatedCostMicros: costMicros,
		Metadata: mustJSON(metadata),
	}, policies)
	if err != nil {
		var capErr *store.UsageCapExceededError
		if errors.As(err, &capErr) || errors.Is(err, store.ErrUsageCapExceeded) {
			blockedPolicy := uuid.Nil
			reason := "cap_exceeded"
			if capErr != nil {
				blockedPolicy = capErr.PolicyID
				if capErr.Reason != "" {
					reason = capErr.Reason
				}
			}
			_ = s.store.InsertUsageCapEvent(ctx, &store.UsageCapEvent{
				TenantID: req.TenantID, PolicyID: optionalPolicyID(blockedPolicy),
				ReservationKey: key, Decision: store.UsageCapEventBlock, Reason: reason,
				EstimatedTokens: usage.TotalTokens(), EstimatedCostMicros: costMicros,
				Metadata: mustJSON(map[string]any{"model_id": req.ModelID, "provider": req.ProviderName}),
			})
			slog.Warn("usage_caps.blocked", "tenant_id", req.TenantID, "policy_id", blockedPolicy, "reason", reason)
			return blockedReservation(req, scope, key, usage, costMicros, blockedPolicy, reason), ErrCapExceeded
		}
		return nil, err
	}
	return &Reservation{
		key: key, result: result, svc: s, usage: usage, prices: prices,
		estimatedCostMicros: costMicros, decision: store.UsageCapEventAllow,
		reason: "reserved", providerName: req.ProviderName,
		providerType: scope.ProviderType, modelID: scope.ModelID,
	}, nil
}

func (r *Reservation) Reconcile(ctx context.Context, resp *providers.ChatResponse, callErr error) {
	r.reconcile(ctx, resp, callErr, false)
}

func (r *Reservation) ReconcileStream(ctx context.Context, resp *providers.ChatResponse, callErr error, streamed bool) {
	r.reconcile(ctx, resp, callErr, streamed)
}

func (r *Reservation) reconcile(ctx context.Context, resp *providers.ChatResponse, callErr error, keepEstimateOnError bool) {
	if r == nil || r.svc == nil || r.key == "" || r.skipped || r.result == nil || len(r.result.Policies) == 0 {
		return
	}
	actual := r.usage
	if resp != nil && resp.Usage != nil {
		actual = pricing.FromProviderUsage(resp.Usage)
		if r.prices.Request == nil {
			actual.RequestCount = 0
		}
		if actual.RequestCount == 0 && r.usage.RequestCount > 0 {
			actual.RequestCount = r.usage.RequestCount
		}
		if actual.ImageCount == 0 {
			actual.ImageCount = r.usage.ImageCount
		}
		if actual.OutputTokens == 0 {
			actual.OutputTokens = r.usage.OutputTokens
		}
		if actual.InputTokens == 0 {
			actual.InputTokens = r.usage.InputTokens
		}
	}
	status := "reconciled"
	if callErr != nil {
		status = "failed"
		if resp == nil || resp.Usage == nil {
			if keepEstimateOnError {
				actual = r.usage
			} else {
				actual = pricing.BillableUsage{}
			}
		}
	}
	cost, err := pricing.CostMicros(r.prices, actual)
	if err != nil {
		cost = r.estimatedCostMicros
	}
	r.actualTokens = actual.TotalTokens()
	r.actualCostMicros = cost
	r.reconcileStatus = status
	reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.svc.store.ReconcileUsage(reconcileCtx, store.UsageReconcileRequest{
		ReservationKey: r.key, ActualTokens: actual.TotalTokens(),
		ActualCostMicros: cost, Status: status,
	}); err != nil {
		slog.Warn("usage_caps.reconcile_failed", "reservation_key", r.key, "error", err)
	}
}

func (s *Service) resolveProvider(ctx context.Context, tenantID uuid.UUID, name string) (*store.LLMProviderData, error) {
	if s.providers == nil || strings.TrimSpace(name) == "" {
		return nil, sql.ErrNoRows
	}
	p, err := s.providers.GetProviderByName(ctx, name)
	if err == nil {
		return p, nil
	}
	if tenantID != uuid.Nil && tenantID != store.MasterTenantID {
		if fallback, fallbackErr := s.providers.GetProviderByName(store.WithTenantID(ctx, store.MasterTenantID), name); fallbackErr == nil {
			return fallback, nil
		}
	}
	return nil, err
}

func ShouldEnforceProvider(providerType string, hasAPIKey bool) bool {
	switch providerType {
	case store.ProviderChatGPTOAuth, store.ProviderClaudeCLI, store.ProviderBailian, store.ProviderACP, store.ProviderOllama:
		return false
	default:
		return hasAPIKey
	}
}

func CountImages(messages []providers.Message) int {
	count := 0
	for _, msg := range messages {
		for _, img := range msg.Images {
			if strings.HasPrefix(strings.ToLower(img.MimeType), "image/") {
				count++
			}
		}
	}
	return count
}

func EstimateInputTokens(messages []providers.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4
		if len(msg.Content)%4 != 0 {
			total++
		}
	}
	if total <= 0 {
		return 1
	}
	return total
}

func requiresCostCap(policies []store.UsageCapPolicy) bool {
	for _, p := range policies {
		if p.MaxCostMicros != nil {
			return true
		}
	}
	return false
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	return b
}

func optionalPolicyID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
