package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// loopbackAddr normalizes a gateway address for local connections.
// CLI processes on the same machine can't connect to 0.0.0.0 on some OSes.
func loopbackAddr(host string, port int) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

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

	if cfg.Providers.Zai.APIKey != "" {
		base := cfg.Providers.Zai.APIBase
		if base == "" {
			base = "https://api.z.ai/api/paas/v4"
		}
		registry.Register(providers.NewOpenAIProvider("zai", cfg.Providers.Zai.APIKey, base, "glm-5"))
		slog.Info("registered provider", "name", "zai")
	}

	if cfg.Providers.ZaiCoding.APIKey != "" {
		base := cfg.Providers.ZaiCoding.APIBase
		if base == "" {
			base = "https://api.z.ai/api/coding/paas/v4"
		}
		registry.Register(providers.NewOpenAIProvider("zai-coding", cfg.Providers.ZaiCoding.APIKey, base, "glm-5"))
		slog.Info("registered provider", "name", "zai-coding")
	}

	// Local / self-hosted Ollama — gated on Host, no API key required.
	// Ollama's OpenAI-compat endpoint accepts any non-empty Bearer value.
	if cfg.Providers.Ollama.Host != "" {
		host := cfg.Providers.Ollama.Host
		registry.Register(providers.NewOpenAIProvider("ollama", "ollama", host+"/v1", "llama3.3"))
		slog.Info("registered provider", "name", "ollama")
	}

	// Ollama Cloud — API key required (generate at ollama.com/settings/keys).
	if cfg.Providers.OllamaCloud.APIKey != "" {
		base := cfg.Providers.OllamaCloud.APIBase
		if base == "" {
			base = "https://ollama.com/v1"
		}
		registry.Register(providers.NewOpenAIProvider("ollama-cloud", cfg.Providers.OllamaCloud.APIKey, base, "llama3.3"))
		slog.Info("registered provider", "name", "ollama-cloud")
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
		// Build per-session MCP config: external MCP servers + GoClaw bridge
		gatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
		mcpData := providers.BuildCLIMCPConfigData(cfg.Tools.McpServers, gatewayAddr, cfg.Gateway.Token)
		opts = append(opts, providers.WithClaudeCLIMCPConfigData(mcpData))
		// Enable GoClaw security hooks (shell deny patterns, path restrictions)
		opts = append(opts, providers.WithClaudeCLISecurityHooks(
			cfg.Providers.ClaudeCLI.BaseWorkDir, true))
		registry.Register(providers.NewClaudeCLIProvider(cliPath, opts...))
		slog.Info("registered provider", "name", "claude-cli")
	}
}

// buildMCPServerLookup creates an MCPServerLookup from an MCPServerStore.
// Returns nil if mcpStore is nil.
func buildMCPServerLookup(mcpStore store.MCPServerStore) providers.MCPServerLookup {
	if mcpStore == nil {
		return nil
	}
	return func(ctx context.Context, agentID string) []providers.MCPServerEntry {
		aid, err := uuid.Parse(agentID)
		if err != nil {
			return nil
		}
		accessible, err := mcpStore.ListAccessible(ctx, aid, "")
		if err != nil {
			slog.Warn("claude-cli: failed to list agent MCP servers", "agent_id", agentID, "error", err)
			return nil
		}
		var entries []providers.MCPServerEntry
		for _, info := range accessible {
			srv := info.Server
			if !srv.Enabled {
				continue
			}
			entry := providers.MCPServerEntry{
				Name:      srv.Name,
				Transport: srv.Transport,
				Command:   srv.Command,
				URL:       srv.URL,
				Args:      jsonToStringSlice(srv.Args),
				Headers:   jsonToStringMap(srv.Headers),
				Env:       jsonToStringMap(srv.Env),
			}
			entries = append(entries, entry)
		}
		return entries
	}
}

// jsonToStringSlice converts a json.RawMessage to []string.
func jsonToStringSlice(data json.RawMessage) []string {
	if len(data) == 0 {
		return nil
	}
	var result []string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// jsonToStringMap converts a json.RawMessage to map[string]string.
func jsonToStringMap(data json.RawMessage) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// registerProvidersFromDB loads providers from Postgres and registers them.
// DB providers are registered after config providers, so they take precedence (overwrite).
// gatewayAddr is used to inject GoClaw MCP bridge for Claude CLI providers.
// mcpStore is optional; when provided, per-agent MCP servers are injected into CLI config.
func registerProvidersFromDB(registry *providers.Registry, provStore store.ProviderStore, secretStore store.ConfigSecretsStore, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore) {
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
			// Validate: only accept "claude" or absolute path
			if cliPath != "claude" && !filepath.IsAbs(cliPath) {
				slog.Warn("security.claude_cli: invalid path from DB, using default", "path", cliPath)
				cliPath = "claude"
			}
			if _, err := exec.LookPath(cliPath); err != nil {
				slog.Warn("claude-cli: binary not found, skipping", "path", cliPath, "error", err)
				continue
			}
			var cliOpts []providers.ClaudeCLIOption
			cliOpts = append(cliOpts, providers.WithClaudeCLISecurityHooks("", true))
			if gatewayAddr != "" {
				mcpData := providers.BuildCLIMCPConfigData(nil, gatewayAddr, gatewayToken)
				mcpData.AgentMCPLookup = buildMCPServerLookup(mcpStore)
				cliOpts = append(cliOpts, providers.WithClaudeCLIMCPConfigData(mcpData))
			}
			registry.Register(providers.NewClaudeCLIProvider(cliPath, cliOpts...))
			slog.Info("registered provider from DB", "name", p.Name)
			continue
		}
		// Local Ollama requires no API key — handle before the key guard (same pattern as ClaudeCLI).
		if p.ProviderType == store.ProviderOllama {
			host := p.APIBase
			if host == "" {
				host = "http://localhost:11434"
			}
			registry.Register(providers.NewOpenAIProvider(p.Name, "ollama", host+"/v1", "llama3.3"))
			slog.Info("registered provider from DB", "name", p.Name)
			continue
		}

		if p.APIKey == "" {
			continue
		}
		switch p.ProviderType {
		case store.ProviderChatGPTOAuth:
			ts := oauth.NewDBTokenSource(provStore, secretStore, p.Name)
			registry.Register(providers.NewCodexProvider(p.Name, ts, p.APIBase, ""))
		case store.ProviderAnthropicNative:
			registry.Register(providers.NewAnthropicProvider(p.APIKey,
				providers.WithAnthropicBaseURL(p.APIBase)))
		case store.ProviderDashScope:
			registry.Register(providers.NewDashScopeProvider(p.APIKey, p.APIBase, ""))
		case store.ProviderBailian:
			base := p.APIBase
			if base == "" {
				base = "https://coding-intl.dashscope.aliyuncs.com/v1"
			}
			registry.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus"))
		case store.ProviderZai:
			base := p.APIBase
			if base == "" {
				base = "https://api.z.ai/api/paas/v4"
			}
			registry.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "glm-5"))
		case store.ProviderZaiCoding:
			base := p.APIBase
			if base == "" {
				base = "https://api.z.ai/api/coding/paas/v4"
			}
			registry.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "glm-5"))
		case store.ProviderOllamaCloud:
			base := p.APIBase
			if base == "" {
				base = "https://ollama.com/v1"
			}
			registry.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "llama3.3"))
		case store.ProviderSuno:
			// Suno is a media-only provider (music gen). Register as OpenAI-compat
			// so credentialProvider interface works for API key/base extraction.
			base := p.APIBase
			if base == "" {
				base = "https://api.sunoapi.org"
			}
			prov := providers.NewOpenAIProvider(p.Name, p.APIKey, base, "")
			prov.WithProviderType(p.ProviderType)
			registry.Register(prov)
		default:
			prov := providers.NewOpenAIProvider(p.Name, p.APIKey, p.APIBase, "")
			prov.WithProviderType(p.ProviderType)
			if p.ProviderType == store.ProviderMiniMax {
				prov.WithChatPath("/text/chatcompletion_v2")
			}
			registry.Register(prov)
		}
		slog.Info("registered provider from DB", "name", p.Name)
	}
}
