package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// loopbackAddr normalizes a gateway address for local connections.
// CLI processes on the same machine can't connect to 0.0.0.0 on some OSes.
func loopbackAddr(host string, port int) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func registerProviders(registry *providers.Registry, cfg *config.Config, modelReg providers.ModelRegistry) {
	gatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
	if cfg.Providers.Anthropic.APIKey != "" {
		registry.Register(providers.NewAnthropicProvider(cfg.Providers.Anthropic.APIKey,
			providers.WithAnthropicBaseURL(cfg.Providers.Anthropic.APIBase),
			providers.WithAnthropicRegistry(modelReg)))
		slog.Info("registered provider", "name", "anthropic")
	}

	if cfg.Providers.OpenAI.APIKey != "" {
		registry.Register(providers.NewOpenAIProvider("openai", cfg.Providers.OpenAI.APIKey, cfg.Providers.OpenAI.APIBase, "gpt-4o").
			WithRegistry(modelReg))
		slog.Info("registered provider", "name", "openai")
	}

	if cfg.Providers.OpenRouter.APIKey != "" {
		orProv := providers.NewOpenAIProvider("openrouter", cfg.Providers.OpenRouter.APIKey, "https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4-5-20250929")
		orProv.WithSiteInfo("https://goclaw.sh", "GoClaw")
		registry.Register(orProv)
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
		registry.Register(providers.NewDashScopeProvider("dashscope", cfg.Providers.DashScope.APIKey, cfg.Providers.DashScope.APIBase, "qwen3-max"))
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

	// Novita AI — OpenAI-compatible endpoint.
	if cfg.Providers.Novita.APIKey != "" {
		base := cfg.Providers.Novita.APIBase
		if base == "" {
			base = store.NovitaDefaultAPIBase
		}
		registry.Register(providers.NewOpenAIProvider("novita", cfg.Providers.Novita.APIKey, base, store.NovitaDefaultModel))
		slog.Info("registered provider", "name", "novita")
	}

	// BytePlus ModelArk — OpenAI-compatible (standard Bearer auth).
	if cfg.Providers.BytePlus.APIKey != "" {
		base := cfg.Providers.BytePlus.APIBase
		if base == "" {
			base = store.BytePlusDefaultAPIBase
		}
		prov := providers.NewOpenAIProvider("byteplus", cfg.Providers.BytePlus.APIKey, base, store.BytePlusDefaultModel)
		prov.WithProviderType(store.ProviderBytePlus)
		registry.Register(prov)
		slog.Info("registered provider", "name", "byteplus")
	}

	// BytePlus ModelArk Coding Plan — separate endpoint for developer tools quota.
	if cfg.Providers.BytePlusCoding.APIKey != "" {
		base := cfg.Providers.BytePlusCoding.APIBase
		if base == "" {
			base = store.BytePlusCodingDefaultAPIBase
		}
		prov := providers.NewOpenAIProvider("byteplus-coding", cfg.Providers.BytePlusCoding.APIKey, base, store.BytePlusDefaultModel)
		prov.WithProviderType(store.ProviderBytePlusCoding)
		registry.Register(prov)
		slog.Info("registered provider", "name", "byteplus-coding")
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
		mcpData := providers.BuildCLIMCPConfigData(cfg.Tools.McpServers, gatewayAddr, cfg.Gateway.Token)
		opts = append(opts, providers.WithClaudeCLIMCPConfigData(mcpData))
		// Enable GoClaw security hooks (shell deny patterns, path restrictions)
		opts = append(opts, providers.WithClaudeCLISecurityHooks(
			cfg.Providers.ClaudeCLI.BaseWorkDir, true))
		registry.Register(providers.NewClaudeCLIProvider(cliPath, opts...))
		slog.Info("registered provider", "name", "claude-cli")
	}

	// ACP provider (config-based) — orchestrates any ACP-compatible agent binary
	if cfg.Providers.ACP.Binary != "" {
		registerACPFromConfig(registry, cfg.Providers.ACP, gatewayAddr, cfg.Gateway.Token, cfg.Agents.Defaults.Workspace)
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
// cfg provides fallback api_base values from config/env when DB providers have none set.
func registerProvidersFromDB(registry *providers.Registry, provStore store.ProviderStore, secretStore store.ConfigSecretsStore, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore, cfg *config.Config, modelReg providers.ModelRegistry) {
	dbProviders, err := provStore.ListAllProviders(context.Background())
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
			cliOpts = append(cliOpts, providers.WithClaudeCLIName(p.Name))
			cliOpts = append(cliOpts, providers.WithClaudeCLISecurityHooks("", true))
			if gatewayAddr != "" {
				mcpData := providers.BuildCLIMCPConfigData(nil, gatewayAddr, gatewayToken)
				mcpData.AgentMCPLookup = buildMCPServerLookup(mcpStore)
				cliOpts = append(cliOpts, providers.WithClaudeCLIMCPConfigData(mcpData))
			}
			registry.RegisterForTenant(p.TenantID, providers.NewClaudeCLIProvider(cliPath, cliOpts...))
			slog.Info("registered provider from DB", "name", p.Name)
			continue
		}
		// ACP provider — no API key needed (agents manage their own auth).
		if p.ProviderType == store.ProviderACP {
			registerACPFromDB(registry, p, gatewayAddr, gatewayToken, buildACPMCPServerLookup(mcpStore), cfg.Agents.Defaults.Workspace)
			continue
		}
		// Local Ollama requires no API key — handle before the key guard (same pattern as ClaudeCLI).
		// api_base is stored with /v1 (normalized at write time), so no suffix appending needed.
		if p.ProviderType == store.ProviderOllama {
			host := p.APIBase
			if host == "" {
				host = "http://localhost:11434/v1"
			}
			registry.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, "ollama", config.DockerLocalhost(host), "llama3.3"))
			slog.Info("registered provider from DB", "name", p.Name)
			continue
		}

		if p.APIKey == "" {
			continue
		}
		// Fall back to config/env api_base when DB provider has none set.
		if p.APIBase == "" && cfg != nil {
			if base := cfg.Providers.APIBaseForType(p.ProviderType); base != "" {
				p.APIBase = base
				slog.Info("provider api_base inherited from config", "name", p.Name, "api_base", base)
			}
		}
		switch p.ProviderType {
		case store.ProviderChatGPTOAuth:
			ts := oauth.NewDBTokenSource(provStore, secretStore, p.Name).WithTenantID(p.TenantID)
			codex := providers.NewCodexProvider(p.Name, ts, p.APIBase, "")
			if oauthSettings := store.ParseChatGPTOAuthProviderSettings(p.Settings); oauthSettings != nil {
				codex.WithRoutingDefaults(oauthSettings.CodexPool.Strategy, oauthSettings.CodexPool.ExtraProviderNames)
			}
			registry.RegisterForTenant(p.TenantID, codex)
		case store.ProviderAnthropicNative:
			registry.RegisterForTenant(p.TenantID, providers.NewAnthropicProvider(p.APIKey,
				providers.WithAnthropicName(p.Name),
				providers.WithAnthropicBaseURL(p.APIBase),
				providers.WithAnthropicRegistry(modelReg)))
		case store.ProviderDashScope:
			registry.RegisterForTenant(p.TenantID, providers.NewDashScopeProvider(p.Name, p.APIKey, p.APIBase, ""))
		case store.ProviderBailian:
			base := p.APIBase
			if base == "" {
				base = "https://coding-intl.dashscope.aliyuncs.com/v1"
			}
			registry.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus"))
		case store.ProviderZai:
			base := p.APIBase
			if base == "" {
				base = "https://api.z.ai/api/paas/v4"
			}
			registry.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, "glm-5"))
		case store.ProviderZaiCoding:
			base := p.APIBase
			if base == "" {
				base = "https://api.z.ai/api/coding/paas/v4"
			}
			registry.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, "glm-5"))
		case store.ProviderOllamaCloud:
			base := p.APIBase
			if base == "" {
				base = "https://ollama.com/v1"
			}
			registry.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, "llama3.3"))
		case store.ProviderNovita:
			base := p.APIBase
			if base == "" {
				base = store.NovitaDefaultAPIBase
			}
			registry.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.NovitaDefaultModel))
		case store.ProviderBytePlus:
			base := p.APIBase
			if base == "" {
				base = store.BytePlusDefaultAPIBase
			}
			prov := providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.BytePlusDefaultModel)
			prov.WithProviderType(p.ProviderType)
			registry.RegisterForTenant(p.TenantID, prov)
		case store.ProviderBytePlusCoding:
			base := p.APIBase
			if base == "" {
				base = store.BytePlusCodingDefaultAPIBase
			}
			prov := providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.BytePlusDefaultModel)
			prov.WithProviderType(p.ProviderType)
			registry.RegisterForTenant(p.TenantID, prov)
		default:
			prov := providers.NewOpenAIProvider(p.Name, p.APIKey, p.APIBase, "")
			prov.WithProviderType(p.ProviderType)
			if p.ProviderType == store.ProviderMiniMax {
				prov.WithChatPath("/text/chatcompletion_v2")
			}
			if p.ProviderType == store.ProviderOpenRouter {
				prov.WithSiteInfo("https://goclaw.sh", "GoClaw")
			}
			registry.RegisterForTenant(p.TenantID, prov)
		}
		slog.Info("registered provider from DB", "name", p.Name)
	}
}

// registerACPFromConfig registers an ACP provider from config file settings.
// workspace is the gateway-level default workspace (cfg.Agents.Defaults.Workspace),
// used to compute candidate directories that gemini exposes via
// --include-directories (per-binary gating happens inside the provider).
func registerACPFromConfig(registry *providers.Registry, cfg config.ACPConfig, gatewayAddr, gatewayToken, workspace string) {
	if _, err := exec.LookPath(cfg.Binary); err != nil {
		slog.Warn("acp: binary not found, skipping", "binary", cfg.Binary, "error", err)
		return
	}
	idleTTL := 5 * time.Minute
	if cfg.IdleTTL != "" {
		if d, err := time.ParseDuration(cfg.IdleTTL); err == nil {
			idleTTL = d
		}
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = defaultACPWorkDir()
	}
	opts := []providers.ACPOption{
		providers.WithIncludeDirectories(acpSkillCandidateDirs(workspace)),
	}
	if cfg.Model != "" {
		opts = append(opts, providers.WithACPModel(cfg.Model))
	}
	if cfg.PermMode != "" {
		opts = append(opts, providers.WithACPPermMode(cfg.PermMode))
	}
	if fn := buildACPMcpServersFunc(gatewayAddr, gatewayToken, nil); fn != nil {
		opts = append(opts, providers.WithACPMcpServersFunc(fn))
	}
	registry.Register(providers.NewACPProvider(
		cfg.Binary, cfg.Args, workDir, idleTTL, tools.DefaultDenyPatterns(), opts...,
	))
	slog.Info("registered provider", "name", "acp", "binary", cfg.Binary, "args", cfg.Args)
}

// buildACPMCPServerLookup returns a lookup function that lists every enabled
// MCP server in the store, ignoring per-agent grants. The UI currently has no
// surface for configuring grants, so treating every enabled server as globally
// available keeps "register a server in the UI → it just shows up in ACP"
// behavior consistent with user expectations. Returns nil when mcpStore is nil.
func buildACPMCPServerLookup(mcpStore store.MCPServerStore) providers.MCPServerLookup {
	if mcpStore == nil {
		return nil
	}
	return func(ctx context.Context, _ string) []providers.MCPServerEntry {
		all, err := mcpStore.ListServers(ctx)
		if err != nil {
			slog.Warn("acp: failed to list MCP servers", "error", err)
			return nil
		}
		var entries []providers.MCPServerEntry
		for _, srv := range all {
			if !srv.Enabled {
				continue
			}
			entries = append(entries, providers.MCPServerEntry{
				Name:      srv.Name,
				Transport: srv.Transport,
				Command:   srv.Command,
				URL:       srv.URL,
				Args:      jsonToStringSlice(srv.Args),
				Headers:   jsonToStringMap(srv.Headers),
				Env:       jsonToStringMap(srv.Env),
			})
		}
		return entries
	}
}

// buildACPMcpServersFunc returns a callback that builds the MCP server list
// for every ACP session/new and session/load request.
//
// The list always includes the GoClaw internal MCP bridge (skill_search,
// web_search, memory_*, etc.). When a per-agent MCPServerLookup is provided,
// UI-registered MCP servers accessible to the calling agent are appended.
// Returns nil when the gateway address is empty (no bridge to advertise).
func buildACPMcpServersFunc(gatewayAddr, gatewayToken string, lookup providers.MCPServerLookup) func(context.Context) []acp.McpServer {
	if gatewayAddr == "" {
		return nil
	}
	bridgeURL := fmt.Sprintf("http://%s/mcp/bridge", gatewayAddr)
	return func(ctx context.Context) []acp.McpServer {
		// Resolve session context; ACP session is created per goclaw session, so
		// these values remain valid for all subsequent MCP tool calls routed
		// through the headers Gemini CLI will replay on each request.
		var agentID, tenantID string
		if aid := store.AgentIDFromContext(ctx); aid != uuid.Nil {
			agentID = aid.String()
		}
		userID := store.UserIDFromContext(ctx)
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
			tenantID = tid.String()
		}
		workspace := tools.ToolWorkspaceFromCtx(ctx)

		// Build headers in the same order and set as Claude CLI bridge, so
		// bridgeContextMiddleware accepts the signature verbatim.
		headers := []acp.McpServerKV{}
		if gatewayToken != "" {
			headers = append(headers, acp.McpServerKV{Name: "Authorization", Value: "Bearer " + gatewayToken})
		}
		if agentID != "" && !strings.ContainsAny(agentID, "\r\n\x00") {
			headers = append(headers, acp.McpServerKV{Name: "X-Agent-ID", Value: agentID})
		}
		if userID != "" && !strings.ContainsAny(userID, "\r\n\x00") {
			headers = append(headers, acp.McpServerKV{Name: "X-User-ID", Value: userID})
		}
		if workspace != "" && !strings.ContainsAny(workspace, "\r\n\x00") {
			headers = append(headers, acp.McpServerKV{Name: "X-Workspace", Value: workspace})
		}
		if tenantID != "" && !strings.ContainsAny(tenantID, "\r\n\x00") {
			headers = append(headers, acp.McpServerKV{Name: "X-Tenant-ID", Value: tenantID})
		}
		// HMAC protects all context fields against forgery. ACP sessions don't
		// carry channel/chatID/peerKind context (those are per-prompt channel
		// routing values), so pass them as empty strings — server verifies
		// with the same empty values and accepts.
		if gatewayToken != "" && (agentID != "" || userID != "") {
			sig := providers.SignBridgeContext(gatewayToken, agentID, userID, "", "", "", workspace, tenantID)
			headers = append(headers, acp.McpServerKV{Name: "X-Bridge-Sig", Value: sig})
		}

		bridgeEntry := acp.McpServerHTTP{
			Type:    "http",
			Name:    "goclaw-bridge",
			URL:     bridgeURL,
			Headers: headers,
		}

		servers := []acp.McpServer{bridgeEntry}
		if lookup == nil {
			return servers
		}
		if agentID == "" {
			return servers
		}
		for _, entry := range lookup(ctx, agentID) {
			servers = append(servers, mcpServerEntryToACP(entry))
		}
		return servers
	}
}

// mcpServerEntryToACP converts a goclaw MCPServerEntry (UI/DB-registered) to
// the ACP schema. Transport strings map as follows:
//   - "stdio"            → McpServerStdio (command/args/env)
//   - "sse"/"http"/other → McpServerHTTP (url + headers, treated as HTTP)
//
// Empty maps/slices are normalized to non-nil so the ACP schema's required
// fields (Headers for HTTP, Args/Env for stdio) are always present.
func mcpServerEntryToACP(e providers.MCPServerEntry) acp.McpServer {
	if e.Transport == "stdio" {
		env := make([]acp.McpServerKV, 0, len(e.Env))
		for k, v := range e.Env {
			env = append(env, acp.McpServerKV{Name: k, Value: v})
		}
		args := e.Args
		if args == nil {
			args = []string{}
		}
		return acp.McpServerStdio{
			Type:    "stdio",
			Name:    e.Name,
			Command: e.Command,
			Args:    args,
			Env:     env,
		}
	}
	headers := make([]acp.McpServerKV, 0, len(e.Headers))
	for k, v := range e.Headers {
		headers = append(headers, acp.McpServerKV{Name: k, Value: v})
	}
	return acp.McpServerHTTP{
		Type:    "http",
		Name:    e.Name,
		URL:     e.URL,
		Headers: headers,
	}
}

// registerACPFromDB registers an ACP provider from a DB provider row.
// lookup may be nil; when provided, UI-registered MCP servers accessible to the
// calling agent are appended to the ACP session's mcpServers.
// workspace is the gateway-level default workspace, used for
// --include-directories on workspace-relative skill slots.
func registerACPFromDB(registry *providers.Registry, p store.LLMProviderData, gatewayAddr, gatewayToken string, lookup providers.MCPServerLookup, workspace string) {
	binary := p.APIBase // repurpose api_base as binary path
	if binary == "" {
		slog.Warn("acp: no binary specified in DB provider", "name", p.Name)
		return
	}
	if binary != "claude" && binary != "codex" && binary != "gemini" && !filepath.IsAbs(binary) {
		slog.Warn("security.acp: invalid binary path from DB", "path", binary)
		return
	}
	if _, err := exec.LookPath(binary); err != nil {
		slog.Warn("acp: binary not found, skipping", "binary", binary, "error", err)
		return
	}
	// Parse settings JSONB for extra config
	var settings struct {
		Args     []string `json:"args"`
		IdleTTL  string   `json:"idle_ttl"`
		PermMode string   `json:"perm_mode"`
		WorkDir  string   `json:"work_dir"`
	}
	if p.Settings != nil {
		if err := json.Unmarshal(p.Settings, &settings); err != nil {
			slog.Warn("acp: invalid settings JSON, using defaults", "name", p.Name, "error", err)
		}
	}
	idleTTL := 5 * time.Minute
	if settings.IdleTTL != "" {
		if d, err := time.ParseDuration(settings.IdleTTL); err == nil {
			idleTTL = d
		}
	}
	workDir := settings.WorkDir
	if workDir == "" {
		workDir = defaultACPWorkDir()
	}
	acpOpts := []providers.ACPOption{
		providers.WithACPName(p.Name),
		providers.WithACPModel(p.Name),
		providers.WithIncludeDirectories(acpSkillCandidateDirs(workspace)),
	}
	if fn := buildACPMcpServersFunc(gatewayAddr, gatewayToken, lookup); fn != nil {
		acpOpts = append(acpOpts, providers.WithACPMcpServersFunc(fn))
	}
	registry.RegisterForTenant(p.TenantID, providers.NewACPProvider(
		binary, settings.Args, workDir, idleTTL, tools.DefaultDenyPatterns(),
		acpOpts...,
	))
	slog.Info("registered provider from DB", "name", p.Name, "type", "acp")
}

// defaultACPWorkDir returns the default workspace directory for ACP agents.
func defaultACPWorkDir() string {
	return filepath.Join(config.ResolvedDataDirFromEnv(), "acp-workspaces")
}

// acpSkillCandidateDirs enumerates the filesystem-backed skill source
// directories that should be exposed to gemini via --include-directories.
// Returns paths regardless of existence; the provider stat-filters before
// emitting flags. Mirrors the five filesystem slots that
// internal/skills/loader.go reads at runtime (builtinSkills is binary-embedded,
// so it is omitted).
//
// Sources mirrored:
//   - workspaceSkills      : <workspace>/skills
//   - projectAgentSkills   : <workspace>/.agents/skills
//   - managedSkillsDir     : <dataDir>/skills-store
//   - globalSkills         : <dataDir>/skills
//   - personalAgentSkills  : ~/.agents/skills
//
// bundled-skills (seeder source) is intentionally NOT included — the seeder
// copies its content into skills-store, so exposing bundled-skills would
// duplicate trees in the agent workspace.
//
// Per-binary gating (only gemini honors --include-directories) and
// vendor-specific defaults like --skip-trust live in providers.NewACPProvider.
// This function only assembles the candidate list.
func acpSkillCandidateDirs(workspace string) []string {
	dataDir := config.ResolvedDataDirFromEnv()
	dirs := []string{
		filepath.Join(dataDir, "skills-store"), // managedSkillsDir
		filepath.Join(dataDir, "skills"),       // globalSkills
	}
	if workspace != "" {
		ws := config.ExpandHome(workspace)
		dirs = append(dirs,
			filepath.Join(ws, "skills"),            // workspaceSkills
			filepath.Join(ws, ".agents", "skills"), // projectAgentSkills
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".agents", "skills"), // personalAgentSkills
		)
	}
	return dirs
}
