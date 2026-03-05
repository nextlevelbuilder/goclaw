package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func registerProviders(registry *providers.Registry, cfg *config.Config) {
	if cfg.Providers.Anthropic.APIKey != "" {
		registry.Register(providers.NewAnthropicProvider(cfg.Providers.Anthropic.APIKey,
			providers.WithAnthropicBaseURL(cfg.Providers.Anthropic.APIBase)))
		slog.Info("registered provider", "name", "anthropic")
	}

	if cfg.Providers.OpenAI.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("openai", cfg.Providers.OpenAI.APIKey, cfg.Providers.OpenAI.APIBase, "gpt-4o"))
		slog.Info("registered provider", "name", "openai")
	}

	if cfg.Providers.OpenRouter.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("openrouter", cfg.Providers.OpenRouter.APIKey, "https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4-5-20250929"))
		slog.Info("registered provider", "name", "openrouter")
	}

	if cfg.Providers.Groq.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("groq", cfg.Providers.Groq.APIKey, "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile"))
		slog.Info("registered provider", "name", "groq")
	}

	if cfg.Providers.DeepSeek.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("deepseek", cfg.Providers.DeepSeek.APIKey, "https://api.deepseek.com/v1", "deepseek-chat"))
		slog.Info("registered provider", "name", "deepseek")
	}

	if cfg.Providers.Gemini.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("gemini", cfg.Providers.Gemini.APIKey, "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash"))
		slog.Info("registered provider", "name", "gemini")
	}

	if cfg.Providers.Mistral.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("mistral", cfg.Providers.Mistral.APIKey, "https://api.mistral.ai/v1", "mistral-large-latest"))
		slog.Info("registered provider", "name", "mistral")
	}

	if cfg.Providers.XAI.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("xai", cfg.Providers.XAI.APIKey, "https://api.x.ai/v1", "grok-3-mini"))
		slog.Info("registered provider", "name", "xai")
	}

	if cfg.Providers.MiniMax.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("minimax", cfg.Providers.MiniMax.APIKey, "https://api.minimax.io/v1", "MiniMax-M2.5").
			WithChatPath("/text/chatcompletion_v2"))
		slog.Info("registered provider", "name", "minimax")
	}

	if cfg.Providers.Cohere.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("cohere", cfg.Providers.Cohere.APIKey, "https://api.cohere.ai/compatibility/v1", "command-a"))
		slog.Info("registered provider", "name", "cohere")
	}

	if cfg.Providers.Perplexity.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("perplexity", cfg.Providers.Perplexity.APIKey, "https://api.perplexity.ai", "sonar-pro"))
		slog.Info("registered provider", "name", "perplexity")
	}

	if cfg.Providers.DashScope.APIKey != "" {
		registry.Register(providers.NewDashScopeProvider(cfg.Providers.DashScope.APIKey, cfg.Providers.DashScope.APIBase, "qwen3-max"))
		slog.Info("registered provider", "name", "dashscope")
	}

	if cfg.Providers.Bailian.APIKey != "" {
		base := cfg.Providers.Bailian.APIBase
		if base == "" {
			base = "https://coding-intl.dashscope.aliyuncs.com/v1"
		}
		registry.Register(providers.NewOpenAIProvider("bailian", cfg.Providers.Bailian.APIKey, base, "qwen3.5-plus"))
		slog.Info("registered provider", "name", "bailian")
	}

	// Claude CLI provider (subscription-based, no API key needed)
	if cfg.Providers.ClaudeCLI.CLIPath != "" {
		cliPath := cfg.Providers.ClaudeCLI.CLIPath
		var opts []providers.ClaudeCLIOption
		if cfg.Providers.ClaudeCLI.Model != "" {
			opts = append(opts, providers.WithClaudeCLIModel(cfg.Providers.ClaudeCLI.Model))
		}
		if cfg.Providers.ClaudeCLI.BaseWorkDir != "" {
			opts = append(opts, providers.WithClaudeCLIWorkDir(cfg.Providers.ClaudeCLI.BaseWorkDir))
		}
		if cfg.Providers.ClaudeCLI.PermMode != "" {
			opts = append(opts, providers.WithClaudeCLIPermMode(cfg.Providers.ClaudeCLI.PermMode))
		}
		// Build MCP config: external MCP servers + GoClaw bridge (built-in tools via streamable-http)
		gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
		mcpPath, _, err := providers.BuildCLIMCPConfig(cfg.Tools.McpServers, gatewayAddr)
		if err != nil {
			slog.Warn("failed to build MCP config for claude-cli", "error", err)
		} else if mcpPath != "" {
			opts = append(opts, providers.WithClaudeCLIMCPConfig(mcpPath))
		}
		// Enable GoClaw security hooks (shell deny patterns, path restrictions)
		opts = append(opts, providers.WithClaudeCLISecurityHooks(
			cfg.Providers.ClaudeCLI.BaseWorkDir, true))
		registry.Register(providers.NewClaudeCLIProvider(cliPath, opts...))
		slog.Info("registered provider", "name", "claude-cli")
	}
}

// registerProvidersFromDB loads providers from Postgres and registers them.
// DB providers are registered after config providers, so they take precedence (overwrite).
// gatewayAddr is used to inject GoClaw MCP bridge for Claude CLI providers.
func registerProvidersFromDB(registry *providers.Registry, provStore store.ProviderStore, gatewayAddr string) {
	ctx := context.Background()
	dbProviders, err := provStore.ListProviders(ctx)
	if err != nil {
		slog.Warn("failed to load providers from DB", "error", err)
		return
	}
	for _, p := range dbProviders {
		// Claude CLI doesn't need API key
		if !p.Enabled {
			continue
		}
		if p.ProviderType == store.ProviderClaudeCLI {
			cliPath := p.APIBase // reuse APIBase field for CLI path
			if cliPath == "" {
				cliPath = "claude"
			}
			var cliOpts []providers.ClaudeCLIOption
			cliOpts = append(cliOpts, providers.WithClaudeCLISecurityHooks("", true))
			if gatewayAddr != "" {
				mcpPath, _, mcpErr := providers.BuildCLIMCPConfig(nil, gatewayAddr)
				if mcpErr != nil {
					slog.Warn("failed to build MCP config for db claude-cli", "error", mcpErr)
				} else if mcpPath != "" {
					cliOpts = append(cliOpts, providers.WithClaudeCLIMCPConfig(mcpPath))
				}
			}
			registry.Register(providers.NewClaudeCLIProvider(cliPath, cliOpts...))
			slog.Info("registered provider from DB", "name", p.Name)
			continue
		}
		if p.APIKey == "" {
			continue
		}
		if p.ProviderType == store.ProviderAnthropicNative {
			registry.Register(providers.NewAnthropicProvider(p.APIKey,
				providers.WithAnthropicBaseURL(p.APIBase)))
		} else if p.ProviderType == store.ProviderDashScope {
			registry.Register(providers.NewDashScopeProvider(p.APIKey, p.APIBase, ""))
		} else if p.ProviderType == store.ProviderBailian {
			base := p.APIBase
			if base == "" {
				base = "https://coding-intl.dashscope.aliyuncs.com/v1"
			}
			registry.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus"))
		} else {
			prov := providers.NewOpenAIProvider(p.Name, p.APIKey, p.APIBase, "")
			if p.ProviderType == store.ProviderMiniMax {
				prov.WithChatPath("/text/chatcompletion_v2")
			}
			registry.Register(prov)
		}
		slog.Info("registered provider from DB", "name", p.Name)
	}
}
