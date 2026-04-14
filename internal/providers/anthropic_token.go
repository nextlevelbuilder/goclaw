package providers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// anthropicAPIKeyPrefix is the prefix for Anthropic API keys.
	anthropicAPIKeyPrefix = "sk-ant-api"
	// anthropicSetupTokenPrefix is the prefix for Anthropic OAuth setup tokens.
	anthropicSetupTokenPrefix = "sk-"
	// anthropicSetupTokenMinLen is the minimum length for a valid setup token.
	anthropicSetupTokenMinLen = 60
	// anthropicSetupTokenExpiry is the default lifetime for setup tokens.
	anthropicSetupTokenExpiry = 365 * 24 * time.Hour
)

// IsAnthropicSetupToken returns true if the key is an Anthropic OAuth setup token.
func IsAnthropicSetupToken(key string) bool {
	return strings.HasPrefix(key, anthropicSetupTokenPrefix) && len(key) >= anthropicSetupTokenMinLen
}

// IsAnthropicAPIKey returns true if the key is a traditional Anthropic API key.
func IsAnthropicAPIKey(key string) bool {
	return strings.HasPrefix(key, anthropicAPIKeyPrefix)
}

// ValidateAnthropicCredential validates an Anthropic credential (API key or setup token).
// Returns nil if valid, or an error describing the problem.
func ValidateAnthropicCredential(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("credential is empty")
	}
	if strings.HasPrefix(key, anthropicSetupTokenPrefix) {
		if len(key) < anthropicSetupTokenMinLen {
			return fmt.Errorf("setup token too short (got %d chars, need at least %d)", len(key), anthropicSetupTokenMinLen)
		}
		return nil
	}
	if strings.HasPrefix(key, anthropicAPIKeyPrefix) {
		return nil
	}
	return fmt.Errorf("unrecognized Anthropic credential format (expected prefix %q or %q)", anthropicAPIKeyPrefix, anthropicSetupTokenPrefix)
}

// AnthropicTokenSettings stores token metadata in llm_providers.settings JSONB.
type AnthropicTokenSettings struct {
	TokenType string `json:"token_type"`           // "api_key" or "setup_token"
	ExpiresAt int64  `json:"expires_at,omitempty"` // unix timestamp, 0 for API keys
}

// NewSetupTokenSettings creates settings for an Anthropic setup token with 1-year expiry.
func NewSetupTokenSettings() AnthropicTokenSettings {
	return AnthropicTokenSettings{
		TokenType: "setup_token",
		ExpiresAt: time.Now().Add(anthropicSetupTokenExpiry).Unix(),
	}
}

// NewAPIKeySettings creates settings for an Anthropic API key (no expiry).
func NewAPIKeySettings() AnthropicTokenSettings {
	return AnthropicTokenSettings{
		TokenType: "api_key",
	}
}

// SettingsForCredential returns the appropriate settings based on the credential format.
func SettingsForCredential(key string) AnthropicTokenSettings {
	if IsAnthropicSetupToken(key) {
		return NewSetupTokenSettings()
	}
	return NewAPIKeySettings()
}

// MarshalJSON returns the settings as JSON for storage in llm_providers.settings.
func (s AnthropicTokenSettings) MarshalJSON() ([]byte, error) {
	type alias AnthropicTokenSettings
	return json.Marshal(alias(s))
}

// DaysUntilExpiry returns the number of days until the token expires.
// Returns -1 if the token has no expiry (API keys).
func (s AnthropicTokenSettings) DaysUntilExpiry() int {
	if s.ExpiresAt == 0 {
		return -1
	}
	d := time.Until(time.Unix(s.ExpiresAt, 0))
	if d < 0 {
		return 0
	}
	return int(d.Hours() / 24)
}
