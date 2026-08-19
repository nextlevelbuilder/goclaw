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
	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
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

	// webhookURL / webhookMinInterval configure the best-effort budget-threshold
	// webhook (reason goclaw.budget). Empty url disables. Set once at gateway
	// startup via SetAlertWebhook from the same reliability alert config that
	// feeds bgalert.SendWebhook.
	webhookURL         string
	webhookMinInterval int
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

func (s *Service) ResolvePricing(ctx context.Context, tenantID uuid.UUID, providerName, modelID string) (*store.ResolvedUsagePricing, error) {
	if s == nil || s.store == nil {
		return nil, sql.ErrNoRows
	}
	ctx = scopedRequestContext(ctx, Request{TenantID: tenantID, ProviderName: providerName, ModelID: modelID})
	providerData, err := s.resolveProvider(ctx, tenantID, providerName)
	if err != nil {
		return nil, err
	}
	return s.store.ResolvePricing(ctx, tenantID, providerData.ID, providerData.Name, providerData.ProviderType, modelID)
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
		return
	}
	r.checkBudgetThresholds(ctx, cost, actual.TotalTokens())
}

// checkBudgetThresholds fires a budget-threshold warn event (decision='warn',
// reason='budget_threshold', reason_group='goclaw.budget') when a Reconcile
// drives any reserved policy past its warn_at_percent. Each
// (policy, window_start) combination warns at most once, so continuous usage
// past the threshold does not spam events or webhooks.
func (r *Reservation) checkBudgetThresholds(ctx context.Context, actualCostMicros, actualTokens int64) {
	for _, p := range r.result.Policies {
		if p.WarnAtPercent == nil || *p.WarnAtPercent <= 0 {
			continue
		}
		start := windowStart(time.Now().UTC(), p.Window)
		if r.budgetWindowWarned(ctx, p.ID, start) {
			continue
		}
		rows, err := r.svc.store.GetBudgetUsage(ctx, r.result.TenantID, store.BudgetUsageWindow{Window: p.Window})
		if err != nil {
			slog.Warn("usage_caps.warn_threshold_read_failed", "policy_id", p.ID, "error", err)
			continue
		}
		var row *store.BudgetUsageRow
		for i := range rows {
			if rows[i].Policy.ID == p.ID {
				row = &rows[i]
				break
			}
		}
		if row == nil || row.PercentUsed < (*p.WarnAtPercent)/100 {
			continue
		}
		r.emitBudgetWarn(ctx, p, start, actualCostMicros, actualTokens)
	}
}

// budgetWindowWarned reports whether a budget-threshold warn already fired for
// the given policy + window. Falls back to false when the store does not
// implement the optional dedup interface.
func (r *Reservation) budgetWindowWarned(ctx context.Context, policyID uuid.UUID, windowStart time.Time) bool {
	if w, ok := r.svc.store.(store.UsageCapBudgetWarnStore); ok {
		fired, err := w.BudgetWindowWarned(ctx, r.result.TenantID, policyID, windowStart)
		return err == nil && fired
	}
	return false
}

func (r *Reservation) emitBudgetWarn(ctx context.Context, p store.UsageCapPolicy, windowStart time.Time, costMicros, tokens int64) error {
	event := &store.UsageCapEvent{
		TenantID:            r.result.TenantID,
		PolicyID:            &p.ID,
		ReservationKey:      r.key,
		Decision:            store.UsageCapEventWarn,
		Reason:              "budget_threshold",
		EstimatedTokens:     r.usage.TotalTokens(),
		EstimatedCostMicros: r.estimatedCostMicros,
		ActualTokens:        tokens,
		ActualCostMicros:    costMicros,
		Metadata: mustJSON(map[string]any{
			"window_start": windowStart.UTC().Format(time.RFC3339),
			"reason_group": "goclaw.budget",
			"policy_id":    p.ID.String(),
		}),
	}
	if err := r.svc.store.InsertUsageCapEvent(ctx, event); err != nil {
		slog.Warn("usage_caps.warn_event_failed", "policy_id", p.ID, "error", err)
		return err
	}
	slog.Warn("usage_caps.warn_threshold",
		"policy_id", p.ID,
		"window_start", windowStart.Format(time.RFC3339),
		"reason_group", "goclaw.budget",
		"percent_used", rowPercent(p, event.ActualCostMicros, event.ActualTokens))
	r.svc.fireBudgetWebhook(ctx, p, costMicros, tokens)
	return nil
}

// fireBudgetWebhook posts a best-effort webhook for a budget-threshold crossing.
// bgalert.SendWebhook requires a non-nil error and maps unknown reasons to
// 'warning' severity; the reason group (goclaw.budget) rides in the reason arg.
// Config-gated: no-op when the gateway never called SetAlertWebhook.
func (s *Service) fireBudgetWebhook(ctx context.Context, p store.UsageCapPolicy, costMicros, tokens int64) {
	if s == nil || s.webhookURL == "" {
		return
	}
	msg := fmt.Errorf("goclaw.budget threshold reached for policy %s (cost_micros=%d tokens=%d)", p.ID, costMicros, tokens)
	bgalert.SendWebhook(ctx, bgalert.AlertDeps{
		WebhookURL:         s.webhookURL,
		MinIntervalSeconds: s.webhookMinInterval,
	}, "usage_caps", "goclaw.budget", msg)
}

// SetAlertWebhook configures the budget-threshold webhook. Empty url disables.
// Called once at gateway startup from the same alert config that feeds bgalert.
func (s *Service) SetAlertWebhook(url string, minIntervalSeconds int) {
	if s == nil {
		return
	}
	s.webhookURL = url
	s.webhookMinInterval = minIntervalSeconds
}

// rowPercent mirrors the store's BudgetUsageRow.PercentUsed precedence (cost
// limit wins when both are set) for warn-event logging.
func rowPercent(p store.UsageCapPolicy, costMicros, tokens int64) float64 {
	if p.MaxCostMicros != nil && *p.MaxCostMicros > 0 {
		return float64(costMicros) / float64(*p.MaxCostMicros)
	}
	if p.MaxTokens != nil && *p.MaxTokens > 0 {
		return float64(tokens) / float64(*p.MaxTokens)
	}
	return 0
}

// windowStart returns the start of the usage window containing now, mirroring
// the store-side usageWindow used for usage_cap_counters rows.
func windowStart(now time.Time, window string) time.Time {
	now = now.UTC()
	switch window {
	case store.UsageCapWindowDay:
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case store.UsageCapWindowWeek:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(weekday - 1))
	case store.UsageCapWindowMonth:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return now.Truncate(time.Hour)
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
