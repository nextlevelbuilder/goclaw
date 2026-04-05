package oauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type openAITokenMetadata struct {
	AccountID string
	PlanType  string
}

type openAIJWTClaims struct {
	Auth      *openAIAuthClaims `json:"https://api.openai.com/auth"`
	AccountID string            `json:"https://api.openai.com/auth.chatgpt_account_id"`
	PlanType  string            `json:"https://api.openai.com/auth.chatgpt_plan_type"`
}

type openAIAuthClaims struct {
	AccountID string `json:"chatgpt_account_id"`
	PlanType  string `json:"chatgpt_plan_type"`
}

func parseOAuthSettings(raw json.RawMessage) OAuthSettings {
	if len(raw) == 0 {
		return OAuthSettings{}
	}
	var settings OAuthSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return OAuthSettings{}
	}
	return settings
}

func mergeOAuthSettings(existing OAuthSettings, tokenResp *OpenAITokenResponse, expiresAt time.Time) OAuthSettings {
	settings := existing
	settings.ExpiresAt = expiresAt.Unix()

	if scope := strings.TrimSpace(tokenResp.Scope); scope != "" {
		settings.Scopes = scope
	}

	metadata := openAIMetadataFromTokenResponse(tokenResp)
	if metadata.AccountID != "" {
		settings.AccountID = metadata.AccountID
	}
	if metadata.PlanType != "" {
		settings.PlanType = metadata.PlanType
	}

	return settings
}

func openAIMetadataFromTokenResponse(tokenResp *OpenAITokenResponse) openAITokenMetadata {
	for _, token := range []string{tokenResp.IDToken, tokenResp.AccessToken} {
		metadata, ok := parseOpenAIJWTMetadata(token)
		if ok {
			return metadata
		}
	}
	return openAITokenMetadata{}
}

func parseOpenAIJWTMetadata(token string) (openAITokenMetadata, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return openAITokenMetadata{}, false
	}

	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return openAITokenMetadata{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return openAITokenMetadata{}, false
	}

	var claims openAIJWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return openAITokenMetadata{}, false
	}

	metadata := openAITokenMetadata{
		AccountID: firstNonEmpty(
			strings.TrimSpace(claims.AccountID),
			strings.TrimSpace(claims.NestedAccountID()),
		),
		PlanType: firstNonEmpty(
			strings.TrimSpace(claims.PlanType),
			strings.TrimSpace(claims.NestedPlanType()),
		),
	}

	if metadata.AccountID == "" && metadata.PlanType == "" {
		return openAITokenMetadata{}, false
	}

	return metadata, true
}

func (c openAIJWTClaims) NestedAccountID() string {
	if c.Auth == nil {
		return ""
	}
	return c.Auth.AccountID
}

func (c openAIJWTClaims) NestedPlanType() string {
	if c.Auth == nil {
		return ""
	}
	return c.Auth.PlanType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
