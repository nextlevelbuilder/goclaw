package providers

import (
	"net/http"
	"strings"
)

// OpenAIProvider implements Provider for OpenAI-compatible APIs
// (OpenAI, Groq, OpenRouter, DeepSeek, VLLM, etc.)
type OpenAIProvider struct {
	name         string
	apiKey       string
	apiBase      string
	chatPath     string // defaults to "/chat/completions"
	authPrefix   string // auth header prefix, defaults to "Bearer " if empty
	defaultModel string
	providerType string // DB provider_type (e.g. "gemini_native", "openai", "minimax_native")
	siteURL      string // optional site URL for provider identification (e.g. OpenRouter HTTP-Referer)
	siteTitle    string // optional site title for provider identification (e.g. OpenRouter X-Title)
	client       *http.Client
	retryConfig  RetryConfig
	middlewares  RequestMiddleware // composed middleware chain (nil = no-op)
	registry     ModelRegistry    // model resolution registry (nil = skip)
}

func NewOpenAIProvider(name, apiKey, apiBase, defaultModel string) *OpenAIProvider {
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	return &OpenAIProvider{
		name:         name,
		apiKey:       apiKey,
		apiBase:      apiBase,
		chatPath:     "/chat/completions",
		defaultModel: defaultModel,
		client:       &http.Client{Timeout: DefaultHTTPTimeout},
		retryConfig:  DefaultRetryConfig(),
		middlewares:  ComposeMiddlewares(FastModeMiddleware, ServiceTierMiddleware, CacheMiddleware),
	}
}

// WithChatPath returns a copy with a custom chat completions path (e.g. "/text/chatcompletion_v2" for MiniMax native API).
func (p *OpenAIProvider) WithChatPath(path string) *OpenAIProvider {
	p.chatPath = path
	return p
}

// WithAuthPrefix sets a custom Authorization header prefix for providers with non-standard auth formats.
// Default is "Bearer " if not set.
func (p *OpenAIProvider) WithAuthPrefix(prefix string) *OpenAIProvider {
	p.authPrefix = prefix
	return p
}

// WithSiteInfo sets site identification headers sent with API requests.
// Used by OpenRouter for rankings (HTTP-Referer, X-Title).
func (p *OpenAIProvider) WithSiteInfo(url, title string) *OpenAIProvider {
	p.siteURL = url
	p.siteTitle = title
	return p
}

// WithRegistry sets the model registry for forward-compat resolution.
func (p *OpenAIProvider) WithRegistry(r ModelRegistry) *OpenAIProvider {
	p.registry = r
	return p
}

// WithMiddlewares sets the composed request middleware chain.
func (p *OpenAIProvider) WithMiddlewares(mws ...RequestMiddleware) *OpenAIProvider {
	p.middlewares = ComposeMiddlewares(mws...)
	return p
}

// WithProviderType sets the DB provider_type for correct API endpoint routing in media tools.
func (p *OpenAIProvider) WithProviderType(pt string) *OpenAIProvider {
	p.providerType = pt
	return p
}

func (p *OpenAIProvider) Name() string           { return p.name }
func (p *OpenAIProvider) DefaultModel() string   { return p.defaultModel }
func (p *OpenAIProvider) SupportsThinking() bool { return true }
func (p *OpenAIProvider) APIKey() string         { return p.apiKey }
func (p *OpenAIProvider) APIBase() string        { return p.apiBase }
func (p *OpenAIProvider) AuthPrefix() string     { return p.authPrefix }
func (p *OpenAIProvider) ProviderType() string   { return p.providerType }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *OpenAIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           true,
		CacheControl:     false,
		MaxContextWindow: 128_000,
		TokenizerID:      "o200k_base",
	}
}

// middlewareConfig builds a MiddlewareConfig from provider fields and the current request.
func (p *OpenAIProvider) middlewareConfig(model string, req ChatRequest) MiddlewareConfig {
	return MiddlewareConfig{
		Provider: p.name,
		Model:    model,
		Caps:     p.Capabilities(),
		AuthType: "api_key",
		APIBase:  p.apiBase,
		Options:  req.Options,
	}
}

// schemaProviderName returns the most specific provider identifier for schema normalization.
// Prefers providerType (from DB) over name for accurate profile matching.
func (p *OpenAIProvider) schemaProviderName() string {
	if p.providerType != "" {
		return p.providerType
	}
	return p.name
}

// resolveModel returns the model ID to use for a request.
// For OpenRouter, model IDs require a provider prefix (e.g. "anthropic/claude-sonnet-4-5-20250929").
// If the caller passes an unprefixed model, fall back to the provider's default.
// After alias resolution, checks the registry for forward-compat specs.
func (p *OpenAIProvider) resolveModel(model string) string {
	if model == "" {
		return p.defaultModel
	}
	if p.name == "openrouter" && !strings.Contains(model, "/") {
		return p.defaultModel
	}
	// Trigger forward-compat resolution to cache specs for token counting.
	// The model ID itself is unchanged — we don't rename models.
	if p.registry != nil {
		_ = p.registry.Resolve("openai", model)
	}
	return model
}
