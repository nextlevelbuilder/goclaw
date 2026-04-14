package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsAnthropicSetupToken(t *testing.T) {
	// Valid: correct prefix + long enough
	validToken := "sk-" + strings.Repeat("A", 80)
	if !IsAnthropicSetupToken(validToken) {
		t.Error("expected valid setup token to be recognized")
	}

	// Invalid: correct prefix but too short
	shortToken := "sk-short"
	if IsAnthropicSetupToken(shortToken) {
		t.Error("expected short token to be rejected")
	}

	// Invalid: wrong prefix
	apiKey := "sk-ant-api03-" + strings.Repeat("B", 80)
	if IsAnthropicSetupToken(apiKey) {
		t.Error("expected API key to not be recognized as setup token")
	}

	// Invalid: empty
	if IsAnthropicSetupToken("") {
		t.Error("expected empty string to be rejected")
	}
}

func TestIsAnthropicAPIKey(t *testing.T) {
	if !IsAnthropicAPIKey("sk-ant-api03-abc123") {
		t.Error("expected API key to be recognized")
	}
	if IsAnthropicAPIKey("sk-abc123") {
		t.Error("expected setup token to not match API key pattern")
	}
	if IsAnthropicAPIKey("random-string") {
		t.Error("expected random string to not match")
	}
}

func TestValidateAnthropicCredential(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid API key", "sk-ant-api03-abc123", false},
		{"valid setup token", "sk-" + strings.Repeat("X", 70), false},
		{"setup token too short", "sk-short", true},
		{"wrong prefix", "sk-other-key-123", true},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"whitespace-padded valid key", "  sk-ant-api03-abc123  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAnthropicCredential(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAnthropicCredential(%q) error = %v, wantErr = %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestAnthropicTokenSettings(t *testing.T) {
	// Setup token settings should have expiry ~1 year from now
	s := NewSetupTokenSettings()
	if s.TokenType != "setup_token" {
		t.Errorf("expected token_type=setup_token, got %s", s.TokenType)
	}
	if s.ExpiresAt == 0 {
		t.Error("expected non-zero expires_at for setup token")
	}
	days := s.DaysUntilExpiry()
	if days < 360 || days > 366 {
		t.Errorf("expected ~365 days until expiry, got %d", days)
	}

	// API key settings should have no expiry
	a := NewAPIKeySettings()
	if a.TokenType != "api_key" {
		t.Errorf("expected token_type=api_key, got %s", a.TokenType)
	}
	if a.ExpiresAt != 0 {
		t.Errorf("expected zero expires_at for API key, got %d", a.ExpiresAt)
	}
	if a.DaysUntilExpiry() != -1 {
		t.Errorf("expected -1 days for API key, got %d", a.DaysUntilExpiry())
	}
}

func TestDaysUntilExpiry(t *testing.T) {
	// Expired token
	expired := AnthropicTokenSettings{ExpiresAt: time.Now().Add(-24 * time.Hour).Unix()}
	if expired.DaysUntilExpiry() != 0 {
		t.Errorf("expected 0 for expired token, got %d", expired.DaysUntilExpiry())
	}

	// Token expiring in 15 days
	soon := AnthropicTokenSettings{ExpiresAt: time.Now().Add(15 * 24 * time.Hour).Unix()}
	days := soon.DaysUntilExpiry()
	if days < 14 || days > 16 {
		t.Errorf("expected ~15 days, got %d", days)
	}

	// No expiry (API key)
	noExpiry := AnthropicTokenSettings{}
	if noExpiry.DaysUntilExpiry() != -1 {
		t.Errorf("expected -1, got %d", noExpiry.DaysUntilExpiry())
	}
}

func TestSettingsForCredential(t *testing.T) {
	// Setup token → setup_token settings
	s := SettingsForCredential("sk-" + strings.Repeat("Z", 70))
	if s.TokenType != "setup_token" {
		t.Errorf("expected setup_token, got %s", s.TokenType)
	}

	// API key → api_key settings
	a := SettingsForCredential("sk-ant-api03-abc123")
	if a.TokenType != "api_key" {
		t.Errorf("expected api_key, got %s", a.TokenType)
	}
}

// TestAnthropicDoRequestHeaders verifies doRequest sets correct HTTP headers
// based on credential type (API key vs OAuth setup token).
func TestAnthropicDoRequestHeaders(t *testing.T) {
	// Minimal valid Anthropic response
	const anthropicOK = `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

	apiKey := "sk-ant-api03-testkey123"
	setupToken := "sk-" + strings.Repeat("T", 80)

	tests := []struct {
		name              string
		apiKey            string
		body              any // passed to doRequest
		wantXAPIKey       bool
		wantBearer        bool
		wantBetaFlags     []string // substrings expected in anthropic-beta header
		wantBrowserAccess bool
	}{
		{
			name:        "api_key_sets_x-api-key",
			apiKey:      apiKey,
			body:        map[string]any{"model": "claude-sonnet-4-5-20250929", "messages": []any{}},
			wantXAPIKey: true,
		},
		{
			name:              "setup_token_sets_bearer_and_oauth_beta",
			apiKey:            setupToken,
			body:              map[string]any{"model": "claude-sonnet-4-5-20250929", "messages": []any{}},
			wantBearer:        true,
			wantBetaFlags:     []string{"claude-code-20250219", "oauth-2025-04-20"},
			wantBrowserAccess: true,
		},
		{
			name:   "setup_token_with_thinking_combines_beta_flags",
			apiKey: setupToken,
			body: map[string]any{
				"model":    "claude-sonnet-4-5-20250929",
				"messages": []any{},
				"thinking": map[string]any{"type": "enabled", "budget_tokens": 10000},
			},
			wantBearer:        true,
			wantBetaFlags:     []string{"claude-code-20250219", "oauth-2025-04-20", "interleaved-thinking-2025-05-14"},
			wantBrowserAccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(anthropicOK))
			}))
			t.Cleanup(srv.Close)

			p := NewAnthropicProvider(tt.apiKey, WithAnthropicBaseURL(srv.URL))
			p.retryConfig.Attempts = 1

			resp, err := p.doRequest(context.Background(), tt.body)
			if err != nil {
				t.Fatalf("doRequest failed: %v", err)
			}
			resp.Close()

			// x-api-key
			if tt.wantXAPIKey {
				if got := captured.Get("X-Api-Key"); got != tt.apiKey {
					t.Errorf("x-api-key = %q, want %q", got, tt.apiKey)
				}
				if got := captured.Get("Authorization"); got != "" {
					t.Errorf("expected no Authorization header for API key, got %q", got)
				}
			}

			// Bearer auth
			if tt.wantBearer {
				want := "Bearer " + tt.apiKey
				if got := captured.Get("Authorization"); got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
				if got := captured.Get("X-Api-Key"); got != "" {
					t.Errorf("expected no x-api-key for setup token, got %q", got)
				}
			}

			// Beta flags
			if len(tt.wantBetaFlags) > 0 {
				beta := captured.Get("Anthropic-Beta")
				for _, flag := range tt.wantBetaFlags {
					if !strings.Contains(beta, flag) {
						t.Errorf("anthropic-beta %q missing flag %q", beta, flag)
					}
				}
			}

			// Browser access
			if tt.wantBrowserAccess {
				if got := captured.Get("Anthropic-Dangerous-Direct-Browser-Access"); got != "true" {
					t.Errorf("anthropic-dangerous-direct-browser-access = %q, want \"true\"", got)
				}
			} else {
				if got := captured.Get("Anthropic-Dangerous-Direct-Browser-Access"); got != "" {
					t.Errorf("expected no anthropic-dangerous-direct-browser-access for API key, got %q", got)
				}
			}
		})
	}
}
