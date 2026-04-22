package store

import (
	"context"
	"encoding/json"

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
	ProviderChatGPTOAuth    = "chatgpt_oauth"
	ProviderClaudeCLI       = "claude_cli"
	ProviderYesScale        = "yescale"
	ProviderZai             = "zai"
	ProviderZaiCoding       = "zai_coding"
	ProviderOllama          = "ollama"       // local or self-hosted Ollama (no API key)
	ProviderOllamaCloud     = "ollama_cloud" // Ollama Cloud (Bearer token required)
	ProviderACP             = "acp"          // ACP (Agent Client Protocol) agent subprocess
	ProviderNovita          = "novita"          // Novita AI (OpenAI-compatible endpoint)
	ProviderBytePlus        = "byteplus"        // BytePlus ModelArk (Seed 2.0 models)
	ProviderBytePlusCoding  = "byteplus_coding" // BytePlus ModelArk Coding Plan

	// Novita AI defaults.
	NovitaDefaultAPIBase = "https://api.novita.ai/openai"
	NovitaDefaultModel   = "moonshotai/kimi-k2.5"

	// BytePlus ModelArk defaults.
	BytePlusDefaultAPIBase       = "https://ark.ap-southeast.bytepluses.com/api/v3"
	BytePlusCodingDefaultAPIBase = "https://ark.ap-southeast.bytepluses.com/api/coding/v3"
	BytePlusDefaultModel         = "seed-2-0-lite-260228"
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
	ProviderChatGPTOAuth:    true,
	ProviderClaudeCLI:       true,
	ProviderYesScale:        true,
	ProviderZai:             true,
	ProviderZaiCoding:       true,
	ProviderOllama:          true,
	ProviderOllamaCloud:     true,
	ProviderACP:             true,
	ProviderNovita:          true,
	ProviderBytePlus:        true,
	ProviderBytePlusCoding:  true,
}

// LLMProviderData represents an LLM provider configuration.
type LLMProviderData struct {
	BaseModel
	TenantID     uuid.UUID       `json:"tenant_id,omitempty" db:"tenant_id"`
	Name         string          `json:"name" db:"name"`
	DisplayName  string          `json:"display_name,omitempty" db:"display_name"`
	ProviderType string          `json:"provider_type" db:"provider_type"`
	APIBase      string          `json:"api_base,omitempty" db:"api_base"`
	APIKey       string          `json:"api_key,omitempty" db:"api_key"`
	Enabled      bool            `json:"enabled" db:"enabled"`
	Settings     json.RawMessage `json:"settings,omitempty" db:"settings"`
}

// RequiredMemoryEmbeddingDimensions is the fixed vector size used by the pgvector memory schema.
// All memory embeddings must match this dimensionality until the schema supports variable sizes.
const RequiredMemoryEmbeddingDimensions = 1536

// EmbeddingSettings holds embedding-specific configuration stored in provider settings JSONB.
type EmbeddingSettings struct {
	Enabled    bool   `json:"enabled" db:"-"`
	Model      string `json:"model,omitempty" db:"-"`      // e.g. "text-embedding-3-small"
	APIBase    string `json:"api_base,omitempty" db:"-"`   // override if embedding endpoint differs from chat
	Dimensions int    `json:"dimensions,omitempty" db:"-"` // truncate output to N dims (e.g. 1536); 0 = model default
}

// ProviderReasoningConfig holds provider-owned default reasoning settings.
// These defaults are inherited by agents unless they save a custom override.
type ProviderReasoningConfig struct {
	Effort   string `json:"effort,omitempty" db:"-"`
	Fallback string `json:"fallback,omitempty" db:"-"`
}

// ChatGPTOAuthProviderSettings holds provider-level defaults for Codex account pooling.
type ChatGPTOAuthProviderSettings struct {
	CodexPool *ChatGPTOAuthRoutingConfig `json:"codex_pool,omitempty" db:"-"`
}

// ParseEmbeddingSettings extracts embedding config from a provider's settings JSONB.
// Returns nil if not configured.
func ParseEmbeddingSettings(settings json.RawMessage) *EmbeddingSettings {
	if len(settings) == 0 {
		return nil
	}
	var s struct {
		Embedding *EmbeddingSettings `json:"embedding"`
	}
	if json.Unmarshal(settings, &s) != nil || s.Embedding == nil {
		return nil
	}
	return s.Embedding
}

// ParseChatGPTOAuthProviderSettings extracts provider-level Codex pool defaults from settings JSONB.
func ParseChatGPTOAuthProviderSettings(settings json.RawMessage) *ChatGPTOAuthProviderSettings {
	if len(settings) == 0 {
		return nil
	}
	var s ChatGPTOAuthProviderSettings
	if json.Unmarshal(settings, &s) != nil {
		return nil
	}
	s.CodexPool = normalizeChatGPTOAuthRoutingConfig(s.CodexPool)
	if s.CodexPool == nil {
		return nil
	}
	s.CodexPool.OverrideMode = ""
	return &s
}

// ParseProviderReasoningConfig extracts provider-owned reasoning defaults from settings JSONB.
// Returns nil when no non-default provider reasoning is configured.
func ParseProviderReasoningConfig(settings json.RawMessage) *ProviderReasoningConfig {
	if len(settings) == 0 {
		return nil
	}
	var raw struct {
		ReasoningDefaults *ProviderReasoningConfig `json:"reasoning_defaults"`
	}
	if json.Unmarshal(settings, &raw) != nil {
		return nil
	}
	return normalizeProviderReasoningConfig(raw.ReasoningDefaults)
}

func normalizeProviderReasoningConfig(raw *ProviderReasoningConfig) *ProviderReasoningConfig {
	if raw == nil {
		return nil
	}
	cfg := &ProviderReasoningConfig{
		Effort:   normalizeReasoningEffort(raw.Effort),
		Fallback: normalizeReasoningFallback(raw.Fallback),
	}
	if cfg.Effort == "" {
		cfg.Effort = "off"
	}
	if cfg.Effort == "off" && cfg.Fallback == ReasoningFallbackDowngrade {
		return nil
	}
	return cfg
}

// NoEmbeddingTypes lists provider types that cannot serve embeddings.
var NoEmbeddingTypes = map[string]bool{
	ProviderAnthropicNative: true, // uses x-api-key auth, not Bearer; no embedding models
	ProviderACP:             true,
	ProviderClaudeCLI:       true,
	ProviderChatGPTOAuth:    true,
}

// ProviderStore manages LLM providers.
type ProviderStore interface {
	CreateProvider(ctx context.Context, p *LLMProviderData) error
	GetProvider(ctx context.Context, id uuid.UUID) (*LLMProviderData, error)
	GetProviderByName(ctx context.Context, name string) (*LLMProviderData, error)
	ListProviders(ctx context.Context) ([]LLMProviderData, error)
	ListAllProviders(ctx context.Context) ([]LLMProviderData, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteProvider(ctx context.Context, id uuid.UUID) error
}
