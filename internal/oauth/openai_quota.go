package oauth

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	openAIQuotaTimeout   = 12 * time.Second
	openAIQuotaUserAgent = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"
)

type OpenAIQuotaResult struct {
	ProviderName string                       `json:"provider_name"`
	Success      bool                         `json:"success"`
	PlanType     string                       `json:"plan_type,omitempty"`
	Windows      []OpenAIQuotaWindow          `json:"windows"`
	CoreUsage    *OpenAIQuotaCoreUsageSummary `json:"core_usage,omitempty"`
	LastUpdated  time.Time                    `json:"last_updated"`
	Error        string                       `json:"error,omitempty"`
	ErrorCode    string                       `json:"error_code,omitempty"`
	ActionHint   string                       `json:"action_hint,omitempty"`
	NeedsReauth  bool                         `json:"needs_reauth,omitempty"`
	IsForbidden  bool                         `json:"is_forbidden,omitempty"`
	Retryable    bool                         `json:"retryable,omitempty"`
}

type OpenAIQuotaWindow struct {
	Label             string  `json:"label"`
	UsedPercent       int     `json:"used_percent"`
	RemainingPercent  int     `json:"remaining_percent"`
	ResetAfterSeconds *int    `json:"reset_after_seconds"`
	ResetAt           *string `json:"reset_at"`
}

type OpenAIQuotaCoreUsageSummary struct {
	FiveHour *OpenAIQuotaCoreUsageWindow `json:"five_hour"`
	Weekly   *OpenAIQuotaCoreUsageWindow `json:"weekly"`
}

type OpenAIQuotaCoreUsageWindow struct {
	Label             string  `json:"label"`
	RemainingPercent  int     `json:"remaining_percent"`
	ResetAfterSeconds *int    `json:"reset_after_seconds"`
	ResetAt           *string `json:"reset_at"`
}

type openAIUsageResponse struct {
	PlanType            string              `json:"plan_type"`
	PlanTypeCamel       string              `json:"planType"`
	RateLimit           *openAIUsageWindows `json:"rate_limit"`
	RateLimitCamel      *openAIUsageWindows `json:"rateLimit"`
	CodeReviewRateLimit *openAIUsageWindows `json:"code_review_rate_limit"`
	CodeReviewCamel     *openAIUsageWindows `json:"codeReviewRateLimit"`
}

type openAIUsageWindows struct {
	PrimaryWindow   *openAIUsageWindow `json:"primary_window"`
	PrimaryCamel    *openAIUsageWindow `json:"primaryWindow"`
	SecondaryWindow *openAIUsageWindow `json:"secondary_window"`
	SecondaryCamel  *openAIUsageWindow `json:"secondaryWindow"`
}

type openAIUsageWindow struct {
	UsedPercent       *float64 `json:"used_percent"`
	UsedPercentCamel  *float64 `json:"usedPercent"`
	ResetAfterSeconds *int     `json:"reset_after_seconds"`
	ResetAfterCamel   *int     `json:"resetAfterSeconds"`
}

func FetchOpenAIQuota(ctx context.Context, provider *store.LLMProviderData, tokenSource *DBTokenSource) OpenAIQuotaResult {
	settings := parseOAuthSettings(provider.Settings)
	result := OpenAIQuotaResult{
		ProviderName: provider.Name,
		PlanType:     normalizeOpenAIPlanType(firstNonEmpty(settings.PlanType)),
		Windows:      []OpenAIQuotaWindow{},
		LastUpdated:  time.Now().UTC(),
	}

	if strings.TrimSpace(settings.AccountID) == "" {
		if tokenSource != nil {
			updatedProvider, err := tokenSource.BackfillProviderMetadata(ctx, provider)
			if err != nil {
				slog.Warn("oauth quota metadata backfill failed", "provider", provider.Name, "error", err)
			} else if updatedProvider != nil {
				provider = updatedProvider
				settings = parseOAuthSettings(provider.Settings)
				result.PlanType = normalizeOpenAIPlanType(firstNonEmpty(settings.PlanType))
			}
		}
	}

	if strings.TrimSpace(settings.AccountID) == "" {
		return buildOpenAIQuotaFailure(result, "missing_account_id", "Quota metadata is missing for this account.", "Sign in again so GoClaw can restore the ChatGPT account workspace metadata.", false, false, false)
	}

	token, err := tokenSource.Token()
	if err != nil {
		return buildOpenAIQuotaFailure(result, "reauth_required", "Token expired or invalid.", "Sign in again to refresh this OpenAI Codex account.", true, false, false)
	}

	payload, err := providers.RetryDo(ctx, providers.RetryConfig{
		Attempts: 2,
		MinDelay: 400 * time.Millisecond,
		MaxDelay: 2 * time.Second,
		Jitter:   0.1,
	}, func() (*openAIUsageResponse, error) {
		return requestOpenAIQuota(ctx, provider.APIBase, token, settings.AccountID)
	})
	if err != nil {
		return buildOpenAIQuotaRequestFailure(result, err)
	}

	result.Success = true
	result.PlanType = firstNonEmpty(normalizeOpenAIPlanType(payload.PlanType), normalizeOpenAIPlanType(payload.PlanTypeCamel), result.PlanType)
	result.Windows = buildOpenAIQuotaWindows(payload)
	result.CoreUsage = buildOpenAIQuotaCoreUsage(result.Windows)
	return result
}

func OpenAIQuotaRouteEligibility(result OpenAIQuotaResult) providers.RouteEligibility {
	if result.Success {
		signals := openAIQuotaCoreSignals(result)
		if len(signals) == 0 {
			return providers.RouteEligibility{Class: providers.RouteEligibilityUnknown, Reason: "unavailable"}
		}
		for _, signal := range signals {
			if signal.RemainingPercent <= 0 {
				return providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "exhausted"}
			}
		}
		return providers.RouteEligibility{Class: providers.RouteEligibilityHealthy}
	}

	switch {
	case result.NeedsReauth:
		return providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "reauth"}
	case result.IsForbidden:
		return providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "forbidden"}
	case result.ErrorCode == "missing_account_id":
		return providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "needs_setup"}
	case result.ErrorCode == "payment_required":
		return providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "billing"}
	case result.Retryable:
		return providers.RouteEligibility{Class: providers.RouteEligibilityUnknown, Reason: "retry_later"}
	default:
		return providers.RouteEligibility{Class: providers.RouteEligibilityUnknown, Reason: "unavailable"}
	}
}

func openAIQuotaCoreSignals(result OpenAIQuotaResult) []*OpenAIQuotaCoreUsageWindow {
	if result.CoreUsage != nil {
		signals := make([]*OpenAIQuotaCoreUsageWindow, 0, 2)
		if result.CoreUsage.FiveHour != nil {
			signals = append(signals, result.CoreUsage.FiveHour)
		}
		if result.CoreUsage.Weekly != nil {
			signals = append(signals, result.CoreUsage.Weekly)
		}
		if len(signals) > 0 {
			return signals
		}
	}

	windows := buildOpenAIQuotaCoreUsage(result.Windows)
	if windows == nil {
		return nil
	}

	signals := make([]*OpenAIQuotaCoreUsageWindow, 0, 2)
	if windows.FiveHour != nil {
		signals = append(signals, windows.FiveHour)
	}
	if windows.Weekly != nil {
		signals = append(signals, windows.Weekly)
	}
	return signals
}

func buildOpenAIQuotaWindows(payload *openAIUsageResponse) []OpenAIQuotaWindow {
	windows := make([]OpenAIQuotaWindow, 0, 4)
	appendWindow := func(label string, window *openAIUsageWindow) {
		if window == nil {
			return
		}
		usedPercent := clampPercent(firstFloat(window.UsedPercent, window.UsedPercentCamel))
		resetAfter := firstInt(window.ResetAfterSeconds, window.ResetAfterCamel)
		windows = append(windows, OpenAIQuotaWindow{
			Label:             label,
			UsedPercent:       usedPercent,
			RemainingPercent:  maxInt(0, 100-usedPercent),
			ResetAfterSeconds: resetAfter,
			ResetAt:           resetAtFromSeconds(resetAfter),
		})
	}

	rateLimit := firstUsageWindows(payload.RateLimit, payload.RateLimitCamel)
	codeReview := firstUsageWindows(payload.CodeReviewRateLimit, payload.CodeReviewCamel)

	appendWindow("Primary", firstUsageWindow(rateLimit, true))
	appendWindow("Secondary", firstUsageWindow(rateLimit, false))
	appendWindow("Code Review (Primary)", firstUsageWindow(codeReview, true))
	appendWindow("Code Review (Secondary)", firstUsageWindow(codeReview, false))
	return windows
}

func buildOpenAIQuotaCoreUsage(windows []OpenAIQuotaWindow) *OpenAIQuotaCoreUsageSummary {
	fiveHour := pickOpenAIQuotaWindow(windows, true)
	weekly := pickOpenAIQuotaWindow(windows, false)
	if countUsageWindows(windows) < 2 {
		weekly = nil
	}
	if fiveHour != nil && weekly != nil && fiveHour.Label == weekly.Label && sameResetAt(fiveHour.ResetAt, weekly.ResetAt) {
		weekly = nil
	}
	if fiveHour == nil && weekly == nil {
		return nil
	}
	return &OpenAIQuotaCoreUsageSummary{
		FiveHour: coreUsageWindowFromQuota(fiveHour),
		Weekly:   coreUsageWindowFromQuota(weekly),
	}
}

func normalizeOpenAIPlanType(planType string) string {
	planType = strings.ToLower(strings.TrimSpace(planType))
	return planType
}

func clampPercent(raw float64) int {
	raw = math.Max(0, math.Min(100, raw))
	return int(math.Round(raw))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstFloat(values ...*float64) float64 {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return 0
}

func firstInt(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func resetAtFromSeconds(resetAfter *int) *string {
	if resetAfter == nil || *resetAfter <= 0 {
		return nil
	}
	resetAt := time.Now().UTC().Add(time.Duration(*resetAfter) * time.Second).Format(time.RFC3339)
	return &resetAt
}

func marshalOAuthSettings(settings OAuthSettings) json.RawMessage {
	data, _ := json.Marshal(settings)
	return json.RawMessage(data)
}

func marshalOAuthSettingsInto(raw json.RawMessage, settings OAuthSettings) json.RawMessage {
	if len(raw) == 0 {
		return marshalOAuthSettings(settings)
	}

	next := make(map[string]any)
	if err := json.Unmarshal(raw, &next); err != nil {
		return marshalOAuthSettings(settings)
	}

	next["expires_at"] = settings.ExpiresAt
	if settings.Scopes != "" {
		next["scopes"] = settings.Scopes
	} else {
		delete(next, "scopes")
	}
	if settings.AccountID != "" {
		next["account_id"] = settings.AccountID
	} else {
		delete(next, "account_id")
	}
	if settings.PlanType != "" {
		next["plan_type"] = settings.PlanType
	} else {
		delete(next, "plan_type")
	}

	data, _ := json.Marshal(next)
	return json.RawMessage(data)
}

func firstUsageWindows(values ...*openAIUsageWindows) *openAIUsageWindows {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstUsageWindow(windows *openAIUsageWindows, primary bool) *openAIUsageWindow {
	if windows == nil {
		return nil
	}
	if primary {
		if windows.PrimaryWindow != nil {
			return windows.PrimaryWindow
		}
		return windows.PrimaryCamel
	}
	if windows.SecondaryWindow != nil {
		return windows.SecondaryWindow
	}
	return windows.SecondaryCamel
}

func pickOpenAIQuotaWindow(windows []OpenAIQuotaWindow, pickShortest bool) *OpenAIQuotaWindow {
	var labeled *OpenAIQuotaWindow
	var unknown []*OpenAIQuotaWindow
	for i := range windows {
		window := &windows[i]
		label := strings.ToLower(window.Label)
		if strings.Contains(label, "code review") {
			continue
		}
		if pickShortest && strings.Contains(label, "primary") {
			return window
		}
		if !pickShortest && strings.Contains(label, "secondary") {
			return window
		}
		unknown = append(unknown, window)
		if labeled == nil {
			labeled = window
		}
	}
	if len(unknown) == 0 {
		return labeled
	}
	best := unknown[0]
	for _, window := range unknown[1:] {
		if best.ResetAfterSeconds == nil {
			best = window
			continue
		}
		if window.ResetAfterSeconds == nil {
			continue
		}
		if pickShortest && *window.ResetAfterSeconds < *best.ResetAfterSeconds {
			best = window
		}
		if !pickShortest && *window.ResetAfterSeconds > *best.ResetAfterSeconds {
			best = window
		}
	}
	return best
}

func coreUsageWindowFromQuota(window *OpenAIQuotaWindow) *OpenAIQuotaCoreUsageWindow {
	if window == nil {
		return nil
	}
	return &OpenAIQuotaCoreUsageWindow{
		Label:             window.Label,
		RemainingPercent:  window.RemainingPercent,
		ResetAfterSeconds: window.ResetAfterSeconds,
		ResetAt:           window.ResetAt,
	}
}

func countUsageWindows(windows []OpenAIQuotaWindow) int {
	count := 0
	for _, window := range windows {
		if strings.Contains(strings.ToLower(window.Label), "code review") {
			continue
		}
		count += 1
	}
	return count
}

func sameResetAt(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
