package store

import (
	"context"

	"github.com/google/uuid"
)

// Provider type constants.
const (
	ProviderAnthropicNative = "anthropic_native"
	ProviderOpenAICompat    = "openai_compat"
	ProviderGeminiNative    = "gemini_native"
	ProviderOpenRouter      = "openrouter"
	ProviderGroq            = "groq"
	ProviderDeepSeek        = "deepseek"
	ProviderMistral         = "mistral"
	ProviderXAI             = "xai"
	ProviderMiniMax         = "minimax_native"
	ProviderCohere          = "cohere"
	ProviderPerplexity      = "perplexity"
	ProviderDashScope       = "dashscope"
	ProviderBailian         = "bailian"
)

// ValidProviderTypes lists all accepted provider_type values.
var ValidProviderTypes = map[string]bool{
	ProviderAnthropicNative: true,
	ProviderOpenAICompat:    true,
	ProviderGeminiNative:    true,
	ProviderOpenRouter:      true,
	ProviderGroq:            true,
	ProviderDeepSeek:        true,
	ProviderMistral:         true,
	ProviderXAI:             true,
	ProviderMiniMax:         true,
	ProviderCohere:          true,
	ProviderPerplexity:      true,
	ProviderDashScope:       true,
	ProviderBailian:         true,
}

// LLMProviderData represents an LLM provider configuration.
type LLMProviderData struct {
	BaseModel
	Name         string `json:"name"`
	DisplayName  string `json:"display_name,omitempty"`
	ProviderType string `json:"provider_type"`
	APIBase      string `json:"api_base,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	Enabled      bool   `json:"enabled"`
}

// ProviderStore manages LLM providers.
type ProviderStore interface {
	CreateProvider(ctx context.Context, p *LLMProviderData) error
	GetProvider(ctx context.Context, id uuid.UUID) (*LLMProviderData, error)
	ListProviders(ctx context.Context) ([]LLMProviderData, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteProvider(ctx context.Context, id uuid.UUID) error
}
