package oauth

import (
	"encoding/json"
	"testing"
)

func TestAnthropicProfile_SubscriptionType(t *testing.T) {
	cases := []struct {
		orgType  string
		expected string
	}{
		{"claude_max", "max"},
		{"claude_pro", "pro"},
		{"claude_enterprise", "enterprise"},
		{"claude_team", "team"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tc := range cases {
		p := &AnthropicProfile{}
		p.Organization.OrganizationType = tc.orgType
		if got := p.SubscriptionType(); got != tc.expected {
			t.Errorf("SubscriptionType(%q) = %q, want %q", tc.orgType, got, tc.expected)
		}
	}
}

func TestAnthropicTokenResponse_Unmarshal(t *testing.T) {
	payload := `{
		"access_token": "at_abc",
		"refresh_token": "rt_xyz",
		"expires_in": 3600,
		"token_type": "Bearer",
		"scope": "user:profile user:inference",
		"account": {"uuid": "acc-uuid", "email_address": "user@example.com"},
		"organization": {"uuid": "org-uuid"}
	}`
	var resp AnthropicTokenResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AccessToken != "at_abc" {
		t.Errorf("AccessToken = %q", resp.AccessToken)
	}
	if resp.Account == nil || resp.Account.UUID != "acc-uuid" {
		t.Errorf("Account mismatch: %+v", resp.Account)
	}
	if resp.Organization == nil || resp.Organization.UUID != "org-uuid" {
		t.Errorf("Organization mismatch: %+v", resp.Organization)
	}
}

func TestAnthropicTokenResponse_ToTokenResponse(t *testing.T) {
	src := &AnthropicTokenResponse{
		AccessToken:  "at_abc",
		RefreshToken: "rt_xyz",
		ExpiresIn:    3600,
		TokenType:    "Bearer",
		Scope:        "user:profile user:inference",
	}
	got := src.ToTokenResponse()
	if got.AccessToken != src.AccessToken {
		t.Errorf("AccessToken mismatch: %q", got.AccessToken)
	}
	if got.RefreshToken != src.RefreshToken {
		t.Errorf("RefreshToken mismatch: %q", got.RefreshToken)
	}
	if got.ExpiresIn != src.ExpiresIn {
		t.Errorf("ExpiresIn mismatch: %d", got.ExpiresIn)
	}
	if got.IDToken != "" {
		t.Errorf("IDToken should be empty for Anthropic tokens, got %q", got.IDToken)
	}
}

func TestAnthropicClientID_RequiresEnvInProd(t *testing.T) {
	t.Setenv("GOCLAW_ANTHROPIC_OAUTH_CLIENT_ID", "")
	t.Setenv("GOCLAW_DEV", "")
	if _, err := AnthropicClientID(); err == nil {
		t.Error("expected error when no client ID configured in prod mode")
	}
}

func TestAnthropicClientID_DevFallback(t *testing.T) {
	t.Setenv("GOCLAW_ANTHROPIC_OAUTH_CLIENT_ID", "")
	t.Setenv("GOCLAW_DEV", "true")
	id, err := AnthropicClientID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != devFallbackClientID {
		t.Errorf("expected dev fallback ID, got %q", id)
	}
}

func TestAnthropicClientID_EnvOverride(t *testing.T) {
	t.Setenv("GOCLAW_ANTHROPIC_OAUTH_CLIENT_ID", "custom-client-id")
	t.Setenv("GOCLAW_DEV", "")
	id, err := AnthropicClientID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "custom-client-id" {
		t.Errorf("expected env override, got %q", id)
	}
}
