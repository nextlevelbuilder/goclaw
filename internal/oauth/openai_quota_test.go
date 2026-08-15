package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestRequestOpenAIQuotaUsesExpectedHeaders(t *testing.T) {
	oldClient := quotaHTTPClient
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Fatalf("path = %q, want /backend-api/wham/usage", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct_123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != openAIQuotaUserAgent {
			t.Fatalf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"team","rate_limit":{"primary_window":{"used_percent":20,"reset_after_seconds":60},"secondary_window":{"used_percent":40,"reset_after_seconds":3600}}}`))
	}))
	defer server.Close()

	quotaHTTPClient = server.Client()
	payload, err := requestOpenAIQuota(t.Context(), server.URL+"/backend-api/", "access-token", "acct_123")
	if err != nil {
		t.Fatalf("requestOpenAIQuota() error = %v", err)
	}
	if payload.PlanType != "team" {
		t.Fatalf("PlanType = %q, want team", payload.PlanType)
	}
}

func TestBuildOpenAIQuotaCoreUsageFallsBackToResetWindows(t *testing.T) {
	short := 120
	long := 7200
	summary := buildOpenAIQuotaCoreUsage([]OpenAIQuotaWindow{
		{Label: "Window A", RemainingPercent: 81, ResetAfterSeconds: &short},
		{Label: "Window B", RemainingPercent: 52, ResetAfterSeconds: &long},
	})

	if summary == nil || summary.FiveHour == nil || summary.Weekly == nil {
		t.Fatal("expected both quota summary windows")
	}
	if summary.FiveHour.Label != "Window A" {
		t.Fatalf("FiveHour.Label = %q, want Window A", summary.FiveHour.Label)
	}
	if summary.Weekly.Label != "Window B" {
		t.Fatalf("Weekly.Label = %q, want Window B", summary.Weekly.Label)
	}
}

func TestBuildOpenAIQuotaCoreUsageDoesNotFabricateWeeklyFromSingleWindow(t *testing.T) {
	short := 120
	summary := buildOpenAIQuotaCoreUsage([]OpenAIQuotaWindow{
		{Label: "Primary", RemainingPercent: 81, ResetAfterSeconds: &short},
	})

	if summary == nil || summary.FiveHour == nil {
		t.Fatal("expected five-hour quota summary")
	}
	if summary.Weekly != nil {
		t.Fatalf("Weekly = %#v, want nil", summary.Weekly)
	}
}

func TestBuildOpenAIQuotaHTTPFailurePaymentRequiredDoesNotRequireReauth(t *testing.T) {
	result := buildOpenAIQuotaHTTPFailure(OpenAIQuotaResult{}, http.StatusPaymentRequired, "billing blocked")

	if result.ErrorCode != "payment_required" {
		t.Fatalf("ErrorCode = %q, want payment_required", result.ErrorCode)
	}
	if result.NeedsReauth {
		t.Fatal("NeedsReauth = true, want false")
	}
	if result.Retryable {
		t.Fatal("Retryable = true, want false")
	}
}

func TestOpenAIQuotaRouteEligibilityClassifiesSignals(t *testing.T) {
	result := OpenAIQuotaResult{
		Success: true,
		CoreUsage: &OpenAIQuotaCoreUsageSummary{
			FiveHour: &OpenAIQuotaCoreUsageWindow{RemainingPercent: 72},
			Weekly:   &OpenAIQuotaCoreUsageWindow{RemainingPercent: 58},
		},
	}

	eligibility := OpenAIQuotaRouteEligibility(result)
	if eligibility.Class != providers.RouteEligibilityHealthy {
		t.Fatalf("eligibility.Class = %q, want %q", eligibility.Class, providers.RouteEligibilityHealthy)
	}
}

func TestOpenAIQuotaRouteEligibilityBlocksBillingAndExhausted(t *testing.T) {
	billing := OpenAIQuotaRouteEligibility(OpenAIQuotaResult{
		Success:   false,
		ErrorCode: "payment_required",
	})
	if billing.Class != providers.RouteEligibilityBlocked || billing.Reason != "billing" {
		t.Fatalf("billing eligibility = %#v", billing)
	}

	exhausted := OpenAIQuotaRouteEligibility(OpenAIQuotaResult{
		Success: true,
		CoreUsage: &OpenAIQuotaCoreUsageSummary{
			FiveHour: &OpenAIQuotaCoreUsageWindow{RemainingPercent: 0},
		},
	})
	if exhausted.Class != providers.RouteEligibilityBlocked || exhausted.Reason != "exhausted" {
		t.Fatalf("exhausted eligibility = %#v", exhausted)
	}
}

func TestOpenAIQuotaRouteEligibilityKeepsRetryableFailuresAsUnknown(t *testing.T) {
	eligibility := OpenAIQuotaRouteEligibility(OpenAIQuotaResult{
		Success:   false,
		Retryable: true,
		ErrorCode: "rate_limited",
	})
	if eligibility.Class != providers.RouteEligibilityUnknown || eligibility.Reason != "retry_later" {
		t.Fatalf("eligibility = %#v", eligibility)
	}
}

func TestFetchOpenAIQuotaBackfillsMetadataFromStoredJWT(t *testing.T) {
	oldClient := quotaHTTPClient
	oldRefresh := refreshOpenAITokenFunc
	t.Cleanup(func() { quotaHTTPClient = oldClient })
	t.Cleanup(func() { refreshOpenAITokenFunc = oldRefresh })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got == "" {
			t.Fatal("Authorization header is empty")
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct_jwt" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"team","rate_limit":{"primary_window":{"used_percent":20,"reset_after_seconds":60},"secondary_window":{"used_percent":40,"reset_after_seconds":3600}}}`))
	}))
	defer server.Close()

	quotaHTTPClient = server.Client()
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         DefaultProviderName,
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      server.URL + "/backend-api",
		APIKey:       "jwt-access-token",
		Enabled:      true,
		Settings: marshalOAuthSettings(OAuthSettings{
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}),
	}
	provStore.providers[provider.Name] = provider

	refreshOpenAITokenFunc = func(string) (*OpenAITokenResponse, error) {
		t.Fatal("refreshOpenAITokenFunc should not be called for JWT metadata backfill")
		return nil, nil
	}

	provider.APIKey = testJWT(t, map[string]any{
		"https://api.openai.com/auth.chatgpt_account_id": "acct_jwt",
		"https://api.openai.com/auth.chatgpt_plan_type":  "team",
	})

	result := FetchOpenAIQuota(t.Context(), provider, NewDBTokenSource(provStore, secretStore, provider.Name))
	if !result.Success {
		t.Fatalf("Success = false, want true: %#v", result)
	}

	settings := parseOAuthSettings(provider.Settings)
	if settings.AccountID != "acct_jwt" {
		t.Fatalf("AccountID = %q, want acct_jwt", settings.AccountID)
	}
	if settings.PlanType != "team" {
		t.Fatalf("PlanType = %q, want team", settings.PlanType)
	}
}

func TestFetchOpenAIQuotaBackfillsMetadataFromRefresh(t *testing.T) {
	oldClient := quotaHTTPClient
	oldRefresh := refreshOpenAITokenFunc
	t.Cleanup(func() {
		quotaHTTPClient = oldClient
		refreshOpenAITokenFunc = oldRefresh
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer refreshed-access-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct_refresh" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"team","rate_limit":{"primary_window":{"used_percent":12,"reset_after_seconds":60},"secondary_window":{"used_percent":31,"reset_after_seconds":3600}}}`))
	}))
	defer server.Close()

	quotaHTTPClient = server.Client()
	refreshOpenAITokenFunc = func(refreshToken string) (*OpenAITokenResponse, error) {
		if refreshToken != "refresh-token" {
			t.Fatalf("refresh token = %q, want refresh-token", refreshToken)
		}
		return &OpenAITokenResponse{
			AccessToken:  "refreshed-access-token",
			RefreshToken: "refresh-token-2",
			ExpiresIn:    3600,
			IDToken: testJWT(t, map[string]any{
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_account_id": "acct_refresh",
					"chatgpt_plan_type":  "team",
				},
			}),
		}, nil
	}

	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	secretStore.data[RefreshTokenSecretKey(DefaultProviderName)] = "refresh-token"

	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         DefaultProviderName,
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      server.URL + "/backend-api",
		APIKey:       "opaque-access-token",
		Enabled:      true,
		Settings: marshalOAuthSettings(OAuthSettings{
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}),
	}
	provStore.providers[provider.Name] = provider

	result := FetchOpenAIQuota(t.Context(), provider, NewDBTokenSource(provStore, secretStore, provider.Name))
	if !result.Success {
		t.Fatalf("Success = false, want true: %#v", result)
	}

	settings := parseOAuthSettings(provider.Settings)
	if settings.AccountID != "acct_refresh" {
		t.Fatalf("AccountID = %q, want acct_refresh", settings.AccountID)
	}
	if settings.PlanType != "team" {
		t.Fatalf("PlanType = %q, want team", settings.PlanType)
	}
	if provider.APIKey != "refreshed-access-token" {
		t.Fatalf("APIKey = %q, want refreshed-access-token", provider.APIKey)
	}
}

func TestFetchOpenAIQuotaStillFailsWhenMetadataCannotBeRecovered(t *testing.T) {
	oldRefresh := refreshOpenAITokenFunc
	t.Cleanup(func() { refreshOpenAITokenFunc = oldRefresh })

	refreshOpenAITokenFunc = func(string) (*OpenAITokenResponse, error) {
		return &OpenAITokenResponse{
			AccessToken: "still-opaque",
			ExpiresIn:   3600,
		}, nil
	}

	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	secretStore.data[RefreshTokenSecretKey(DefaultProviderName)] = "refresh-token"

	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         DefaultProviderName,
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      "https://example.com/backend-api",
		APIKey:       "opaque-access-token",
		Enabled:      true,
		Settings: marshalOAuthSettings(OAuthSettings{
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}),
	}
	provStore.providers[provider.Name] = provider

	result := FetchOpenAIQuota(t.Context(), provider, NewDBTokenSource(provStore, secretStore, provider.Name))
	if result.Success {
		t.Fatalf("Success = true, want false: %#v", result)
	}
	if result.ErrorCode != "missing_account_id" {
		t.Fatalf("ErrorCode = %q, want missing_account_id", result.ErrorCode)
	}
}
