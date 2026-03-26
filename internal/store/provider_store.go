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
	ProviderSuno            = "suno"
	ProviderYesScale        = "yescale"
	ProviderZai             = "zai"
	ProviderZaiCoding       = "zai_coding"
	ProviderOllama          = "ollama"       // local or self-hosted Ollama (no API key)
	ProviderOllamaCloud     = "ollama_cloud" // Ollama Cloud (Bearer token required)
	ProviderACP             = "acp"          // ACP (Agent Client Protocol) agent subprocess
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
	ProviderSuno:            true,
	ProviderYesScale:        true,
	ProviderZai:             true,
	ProviderZaiCoding:       true,
	ProviderOllama:          true,
	ProviderOllamaCloud:     true,
	ProviderACP:             true,
}

// LLMProviderData represents an LLM provider configuration.
type LLMProviderData struct {
	BaseModel
	TenantID     uuid.UUID       `json:"tenant_id,omitempty"`
	Name         string          `json:"name"`
	DisplayName  string          `json:"display_name,omitempty"`
	ProviderType string          `json:"provider_type"`
	APIBase      string          `json:"api_base,omitempty"`
	APIKey       string          `json:"api_key,omitempty"`
	Enabled      bool            `json:"enabled"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

// EmbeddingSettings holds embedding-specific configuration stored in provider settings JSONB.
type EmbeddingSettings struct {
	Enabled    bool   `json:"enabled"`
	Model      string `json:"model,omitempty"`      // e.g. "text-embedding-3-small"
	APIBase    string `json:"api_base,omitempty"`    // override if embedding endpoint differs from chat
	Dimensions int    `json:"dimensions,omitempty"` // truncate output to N dims (e.g. 1536); 0 = model default
}

// ChatGPTOAuthProviderSettings holds provider-level defaults for Codex account pooling.
type ChatGPTOAuthProviderSettings struct {
	CodexPool *ChatGPTOAuthRoutingConfig `json:"codex_pool,omitempty"`
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

// NoEmbeddingTypes lists provider types that cannot serve embeddings.
var NoEmbeddingTypes = map[string]bool{
	ProviderAnthropicNative: true, // uses x-api-key auth, not Bearer; no embedding models
	ProviderACP:             true,
	ProviderClaudeCLI:       true,
	ProviderChatGPTOAuth:    true,
	ProviderSuno:            true,
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
