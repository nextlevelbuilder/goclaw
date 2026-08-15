package oauth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestParseOpenAIJWTMetadataNestedClaims(t *testing.T) {
	token := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_nested",
			"chatgpt_plan_type":  "team",
		},
	})

	metadata, ok := parseOpenAIJWTMetadata(token)
	if !ok {
		t.Fatal("parseOpenAIJWTMetadata() = false, want true")
	}
	if metadata.AccountID != "acct_nested" {
		t.Fatalf("AccountID = %q, want acct_nested", metadata.AccountID)
	}
	if metadata.PlanType != "team" {
		t.Fatalf("PlanType = %q, want team", metadata.PlanType)
	}
}

func TestParseOpenAIJWTMetadataFlatClaims(t *testing.T) {
	token := testJWT(t, map[string]any{
		"https://api.openai.com/auth.chatgpt_account_id": "acct_flat",
		"https://api.openai.com/auth.chatgpt_plan_type":  "plus",
	})

	metadata, ok := parseOpenAIJWTMetadata(token)
	if !ok {
		t.Fatal("parseOpenAIJWTMetadata() = false, want true")
	}
	if metadata.AccountID != "acct_flat" {
		t.Fatalf("AccountID = %q, want acct_flat", metadata.AccountID)
	}
	if metadata.PlanType != "plus" {
		t.Fatalf("PlanType = %q, want plus", metadata.PlanType)
	}
}

func TestMergeOAuthSettingsPreservesExistingMetadata(t *testing.T) {
	existing := OAuthSettings{
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
		Scopes:    "openid profile",
		AccountID: "acct_keep",
		PlanType:  "team",
	}

	settings := mergeOAuthSettings(existing, &OpenAITokenResponse{
		AccessToken: "not-a-jwt",
		ExpiresIn:   3600,
	}, time.Now().Add(time.Hour))

	if settings.AccountID != "acct_keep" {
		t.Fatalf("AccountID = %q, want acct_keep", settings.AccountID)
	}
	if settings.PlanType != "team" {
		t.Fatalf("PlanType = %q, want team", settings.PlanType)
	}
	if settings.Scopes != "openid profile" {
		t.Fatalf("Scopes = %q, want openid profile", settings.Scopes)
	}
}

func testJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body) + "."
}
