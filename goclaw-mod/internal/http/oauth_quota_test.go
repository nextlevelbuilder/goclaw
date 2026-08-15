package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestOAuthHandlerQuotaSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct_123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"team","rate_limit":{"primary_window":{"used_percent":24,"reset_after_seconds":60},"secondary_window":{"used_percent":38,"reset_after_seconds":3600}}}`))
	}))
	defer server.Close()

	h, provStore, _ := newTestOAuthHandlerWithStores(t, "secret-token")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	createTestOAuthProvider(t, provStore, "openai-codex", store.ProviderChatGPTOAuth, server.URL, oauthSettingsJSON(t, map[string]any{
		"expires_at": time.Now().Add(time.Hour).Unix(),
		"account_id": "acct_123",
		"plan_type":  "team",
	}), true)
	provStore.providers["openai-codex"].APIKey = "access-token"

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/openai/quota", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["success"] != true {
		t.Fatalf("success = %v, want true", result["success"])
	}
}

func TestOAuthHandlerQuotaMissingProvider(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/chatgpt/missing/quota", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["code"] != "provider_not_found" {
		t.Fatalf("code = %v, want provider_not_found", result["code"])
	}
}

func TestOAuthHandlerQuotaProviderConflict(t *testing.T) {
	h, provStore, _ := newTestOAuthHandlerWithStores(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	createTestOAuthProvider(t, provStore, "openai-codex", store.ProviderOpenRouter, "https://example.com", nil, true)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/openai/quota", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["code"] != "provider_type_conflict" {
		t.Fatalf("code = %v, want provider_type_conflict", result["code"])
	}
}

func TestOAuthHandlerQuotaMissingAccountID(t *testing.T) {
	h, provStore, _ := newTestOAuthHandlerWithStores(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	createTestOAuthProvider(t, provStore, "openai-codex", store.ProviderChatGPTOAuth, "https://example.com", oauthSettingsJSON(t, map[string]any{
		"expires_at": time.Now().Add(time.Hour).Unix(),
	}), true)
	provStore.providers["openai-codex"].APIKey = "access-token"

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/openai/quota", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["success"] != false {
		t.Fatalf("success = %v, want false", result["success"])
	}
	if result["error_code"] != "missing_account_id" {
		t.Fatalf("error_code = %v, want missing_account_id", result["error_code"])
	}
}

func TestOAuthHandlerQuotaForbidden(t *testing.T) {
	testOAuthHandlerQuotaFailureCode(t, http.StatusForbidden, "quota_api_forbidden", "is_forbidden")
}

func TestOAuthHandlerQuotaReauthRequired(t *testing.T) {
	testOAuthHandlerQuotaFailureCode(t, http.StatusUnauthorized, "reauth_required", "needs_reauth")
}

func TestOAuthHandlerQuotaRateLimited(t *testing.T) {
	testOAuthHandlerQuotaFailureCode(t, http.StatusTooManyRequests, "rate_limited", "retryable")
}

func TestOAuthHandlerQuotaPaymentRequired(t *testing.T) {
	testOAuthHandlerQuotaFailureCode(t, http.StatusPaymentRequired, "payment_required", "")
}

func testOAuthHandlerQuotaFailureCode(t *testing.T, status int, errorCode, flag string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream error", status)
	}))
	defer server.Close()

	h, provStore, _ := newTestOAuthHandlerWithStores(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	createTestOAuthProvider(t, provStore, "openai-codex", store.ProviderChatGPTOAuth, server.URL, oauthSettingsJSON(t, map[string]any{
		"expires_at": time.Now().Add(time.Hour).Unix(),
		"account_id": "acct_123",
	}), true)
	provStore.providers["openai-codex"].APIKey = "access-token"

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/openai/quota", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result map[string]any
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["error_code"] != errorCode {
		t.Fatalf("error_code = %v, want %s", result["error_code"], errorCode)
	}
	if flag != "" && result[flag] != true {
		t.Fatalf("%s = %v, want true", flag, result[flag])
	}
	if status == http.StatusPaymentRequired && result["needs_reauth"] == true {
		t.Fatalf("needs_reauth = %v, want false", result["needs_reauth"])
	}
}

func createTestOAuthProvider(t *testing.T, provStore *mockProviderStore, name, providerType, apiBase string, settings json.RawMessage, enabled bool) {
	t.Helper()
	if settings == nil {
		settings = json.RawMessage(`{}`)
	}
	provStore.providers[name] = &store.LLMProviderData{
		Name:         name,
		ProviderType: providerType,
		APIBase:      apiBase,
		APIKey:       "access-token",
		Enabled:      enabled,
		Settings:     settings,
	}
}

func oauthSettingsJSON(t *testing.T, value map[string]any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	return json.RawMessage(data)
}
