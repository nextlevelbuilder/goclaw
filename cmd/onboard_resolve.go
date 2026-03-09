package cmd

import (
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolveProviderAPIKey extracts the API key for a provider from the config.
func resolveProviderAPIKey(cfg *config.Config, providerName string) string {
	switch providerName {
	case "openrouter":
		return cfg.Providers.OpenRouter.APIKey
	case "anthropic":
		return cfg.Providers.Anthropic.APIKey
	case "openai":
		return cfg.Providers.OpenAI.APIKey
	case "groq":
		return cfg.Providers.Groq.APIKey
	case "deepseek":
		return cfg.Providers.DeepSeek.APIKey
	case "gemini":
		return cfg.Providers.Gemini.APIKey
	case "mistral":
		return cfg.Providers.Mistral.APIKey
	case "xai":
		return cfg.Providers.XAI.APIKey
	case "minimax":
		return cfg.Providers.MiniMax.APIKey
	case "cohere":
		return cfg.Providers.Cohere.APIKey
	case "perplexity":
		return cfg.Providers.Perplexity.APIKey
	case "dashscope":
		return cfg.Providers.DashScope.APIKey
	case "bailian":
		return cfg.Providers.Bailian.APIKey
	default:
		return ""
	}
}

// resolveProviderType maps a provider name to its store.Provider* type constant.
func resolveProviderType(name string) string {
	switch name {
	case "anthropic":
		return store.ProviderAnthropicNative
	case "gemini":
		return store.ProviderGeminiNative
	case "minimax":
		return store.ProviderMiniMax
	case "openrouter":
		return store.ProviderOpenRouter
	case "groq":
		return store.ProviderGroq
	case "deepseek":
		return store.ProviderDeepSeek
	case "mistral":
		return store.ProviderMistral
	case "xai":
		return store.ProviderXAI
	case "cohere":
		return store.ProviderCohere
	case "perplexity":
		return store.ProviderPerplexity
	case "yescale":
		return store.ProviderYesScale
	default:
		return store.ProviderOpenAICompat
	}
}

// resolveProviderAPIBase returns the default API base URL for known providers.
func resolveProviderAPIBase(providerName string) string {
	switch providerName {
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "openai":
		return "https://api.openai.com/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "mistral":
		return "https://api.mistral.ai/v1"
	case "xai":
		return "https://api.x.ai/v1"
	case "minimax":
		return "https://api.minimax.io/v1"
	case "cohere":
		return "https://api.cohere.com/v2"
	case "perplexity":
		return "https://api.perplexity.ai"
	case "dashscope":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "bailian":
		return "https://coding-intl.dashscope.aliyuncs.com/v1"
	case "yescale":
		return "https://api.yescale.one/v1"
	default:
		return ""
	}
}
