package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/compat"
	"github.com/nextlevelbuilder/goclaw/internal/providers/wire"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// providerConcurrencyGate builds a providers.ConcurrencyGate backed by a
// dedicated scheduler.Lane for one provider (llm_providers.settings.max_in_flight).
// lanes is namespaced separately from the run-scheduler's lanes
// (main/subagent/team/cron, see internal/scheduler.DefaultLanes) so a
// provider's concurrency cap never competes with, or is confused with,
// per-channel run scheduling. Returns nil (no gate) when lanes is nil or the
// provider declares no cap — Chat/ChatStream then run unbounded exactly as
// before this feature existed.
func providerConcurrencyGate(lanes *scheduler.LaneManager, providerName string, maxInFlight int) providers.ConcurrencyGate {
	if lanes == nil || maxInFlight <= 0 {
		return nil
	}
	lane := lanes.GetOrCreate("provider:"+providerName, maxInFlight)
	return func(ctx context.Context, fn func()) error {
		done := make(chan struct{})
		if err := lane.Submit(ctx, func() {
			defer close(done)
			fn()
		}); err != nil {
			return err
		}
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// loopbackAddr normalizes a gateway address for local connections.
// CLI processes on the same machine can't connect to 0.0.0.0 on some OSes.
func loopbackAddr(host string, port int) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// registerProviders registers the providers declared in the config file / env.
//
// Every transport is constructed by the wire registry from a brand declaration:
// base URLs, default models, identity headers and provider_type reflection rules
// are catalog data (internal/providers/wire/brand.go), so this file contains no
// per-brand construction logic.
func registerProviders(registry *providers.Registry, cfg *config.Config, modelReg providers.ModelRegistry) {
	if cfg == nil {
		return
	}

	// Plain OpenAI-compatible brands: name, brand id, credentials, api_base.
	// Empty credentials mean "not configured" and the brand is skipped.
	for _, spec := range configOpenAICompatBrands(cfg) {
		if spec.apiKey == "" {
			continue
		}
		api, ok := wire.APIForBrand(spec.providerType)
		if !ok {
			slog.Error("provider.brand.unknown", "name", spec.name, "provider_type", spec.providerType)
			continue
		}
		build := wire.Config{
			API:          api,
			Source:       wire.SourceConfig,
			Name:         spec.name,
			ProviderType: spec.providerType,
			APIKey:       spec.apiKey,
			BaseURL:      spec.apiBase,
			DefaultModel: spec.defaultModel,
		}
		if spec.withRegistry {
			build.Registry = modelReg
		}
		registerConfigProvider(registry, build)
	}

	if cfg.Providers.Anthropic.APIKey != "" {
		registerConfigProvider(registry, wire.Config{
			API:          wire.AnthropicMessages,
			Source:       wire.SourceConfig,
			Name:         "anthropic",
			ProviderType: store.ProviderAnthropicNative,
			APIKey:       cfg.Providers.Anthropic.APIKey,
			BaseURL:      cfg.Providers.Anthropic.APIBase,
			Registry:     modelReg,
		})
	}

	registerOllamaFromConfig(registry, cfg)

	// Google Cloud Vertex AI — OAuth2 service account or Application Default Credentials.
	// Registers when project_id + region are set. Credential sources (priority order):
	// inline JSON (APIKey) → file path (CredentialsFile) → ADC.
	if cfg.Providers.Vertex.ProjectID != "" && cfg.Providers.Vertex.Region != "" {
		registerConfigProvider(registry, wire.Config{
			API:          wire.GoogleVertex,
			Source:       wire.SourceConfig,
			Name:         "vertex",
			ProviderType: store.ProviderVertex,
			APIKey:       cfg.Providers.Vertex.APIKey,
			Vertex: &wire.VertexSettings{
				ProjectID:       cfg.Providers.Vertex.ProjectID,
				Region:          cfg.Providers.Vertex.Region,
				Model:           cfg.Providers.Vertex.Model,
				CredentialsFile: cfg.Providers.Vertex.CredentialsFile,
			},
		})
	}

	registerClaudeCLIFromConfig(registry, cfg)

	// ACP provider (config-based) — orchestrates any ACP-compatible agent binary
	if cfg.Providers.ACP.Binary != "" {
		registerACPFromConfig(registry, cfg.Providers.ACP, configuredShellDenyGroups(cfg))
	}
}

// configOpenAICompatBrand specifies a config-declared brand that builds a plain
// transport straight from its config block.
type configOpenAICompatBrand struct {
	name         string
	providerType string
	apiKey       string
	apiBase      string
	// defaultModel overrides the brand default (used where the config path has
	// historically pinned a different model than the vendor default).
	defaultModel string
	// withRegistry mirrors the historical per-brand model-registry wiring.
	withRegistry bool
}

func configOpenAICompatBrands(cfg *config.Config) []configOpenAICompatBrand {
	p := cfg.Providers
	return []configOpenAICompatBrand{
		{name: "openai", providerType: store.ProviderOpenAICompat, apiKey: p.OpenAI.APIKey, apiBase: p.OpenAI.APIBase, withRegistry: true},
		{name: "atlascloud", providerType: store.ProviderAtlasCloud, apiKey: p.AtlasCloud.APIKey, apiBase: p.AtlasCloud.APIBase},
		{name: "api_route", providerType: store.ProviderAPIRoute, apiKey: p.APIRoute.APIKey, apiBase: p.APIRoute.APIBase},
		{name: "requesty", providerType: store.ProviderRequesty, apiKey: p.Requesty.APIKey, apiBase: p.Requesty.APIBase},
		{name: "openrouter", providerType: store.ProviderOpenRouter, apiKey: p.OpenRouter.APIKey, apiBase: p.OpenRouter.APIBase},
		{name: "groq", providerType: store.ProviderGroq, apiKey: p.Groq.APIKey, apiBase: p.Groq.APIBase},
		{name: "deepseek", providerType: store.ProviderDeepSeek, apiKey: p.DeepSeek.APIKey, apiBase: p.DeepSeek.APIBase},
		{name: "gemini", providerType: store.ProviderGeminiNative, apiKey: p.Gemini.APIKey, apiBase: p.Gemini.APIBase},
		{name: "mistral", providerType: store.ProviderMistral, apiKey: p.Mistral.APIKey, apiBase: p.Mistral.APIBase},
		{name: "xai", providerType: store.ProviderXAI, apiKey: p.XAI.APIKey, apiBase: p.XAI.APIBase},
		{name: "minimax", providerType: store.ProviderMiniMax, apiKey: p.MiniMax.APIKey, apiBase: p.MiniMax.APIBase},
		{name: "cohere", providerType: store.ProviderCohere, apiKey: p.Cohere.APIKey, apiBase: p.Cohere.APIBase},
		{name: "perplexity", providerType: store.ProviderPerplexity, apiKey: p.Perplexity.APIKey, apiBase: p.Perplexity.APIBase},
		{name: "dashscope", providerType: store.ProviderDashScope, apiKey: p.DashScope.APIKey, apiBase: p.DashScope.APIBase, defaultModel: "qwen3-max"},
		{name: "bailian", providerType: store.ProviderBailian, apiKey: p.Bailian.APIKey, apiBase: p.Bailian.APIBase},
		{name: "zai", providerType: store.ProviderZai, apiKey: p.Zai.APIKey, apiBase: p.Zai.APIBase},
		{name: "zai-coding", providerType: store.ProviderZaiCoding, apiKey: p.ZaiCoding.APIKey, apiBase: p.ZaiCoding.APIBase},
		{name: "novita", providerType: store.ProviderNovita, apiKey: p.Novita.APIKey, apiBase: p.Novita.APIBase},
		{name: "byteplus", providerType: store.ProviderBytePlus, apiKey: p.BytePlus.APIKey, apiBase: p.BytePlus.APIBase},
		{name: "byteplus-coding", providerType: store.ProviderBytePlusCoding, apiKey: p.BytePlusCoding.APIKey, apiBase: p.BytePlusCoding.APIBase},
	}
}

// registerOllamaFromConfig registers the two native Ollama transports. Both use
// the Ollama Go client (/api/chat), not the OpenAI-compatible shim, so they take
// the ollama-native wire API; local Ollama is gated on Host and needs no key,
// Ollama Cloud is gated on an API key.
func registerOllamaFromConfig(registry *providers.Registry, cfg *config.Config) {
	if cfg.Providers.Ollama.Host != "" {
		host := cfg.Providers.Ollama.Host
		registerConfigProvider(registry, wire.Config{
			API:          wire.OllamaNative,
			Source:       wire.SourceConfig,
			Name:         "ollama",
			ProviderType: store.ProviderOllama,
			BaseURL:      host,
			OllamaNumCtx: probeOllamaNumCtx(config.DockerLocalhost(host)),
		})
	}

	if cfg.Providers.OllamaCloud.APIKey != "" {
		// The probe needs the effective base URL, so resolve the brand default here
		// rather than duplicating it.
		base := cfg.Providers.OllamaCloud.APIBase
		if base == "" {
			base, _, _, _ = wire.BrandDefaults(store.ProviderOllamaCloud)
		}
		registerConfigProvider(registry, wire.Config{
			API:          wire.OllamaNative,
			Source:       wire.SourceConfig,
			Name:         "ollama-cloud",
			ProviderType: store.ProviderOllamaCloud,
			APIKey:       cfg.Providers.OllamaCloud.APIKey,
			BaseURL:      cfg.Providers.OllamaCloud.APIBase,
			OllamaNumCtx: probeOllamaNumCtx(config.DockerLocalhost(base)),
		})
	}
}

// probeOllamaNumCtx asks a reachable Ollama endpoint for a default model's
// context window. A result equal to the built-in default carries no information,
// so it is reported as "unset" and the provider resolves it per model instead.
func probeOllamaNumCtx(apiBase string) *int {
	ctx5s, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	numCtx := providers.FetchOllamaModelContext(ctx5s, apiBase, "llama3.3", "")
	if numCtx == providers.OllamaDefaultNumCtx {
		return nil
	}
	return &numCtx
}

// registerConfigProvider builds a config-declared provider through the wire
// registry. Construction failures are logged and skipped; a broken optional
// provider must not stop the gateway from booting.
func registerConfigProvider(registry *providers.Registry, cfg wire.Config) bool {
	prov, err := wire.Build(cfg)
	if err != nil {
		slog.Error("provider.register.failed", "name", cfg.Name, "wire_api", string(cfg.API), "error", err)
		return false
	}
	registry.Register(prov)
	slog.Info("registered provider", "name", cfg.Name)
	return true
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
//
// Dispatch is on llm_providers.wire_api alone: a row whose wire_api is not
// registered is logged and skipped, never silently downgraded to an
// OpenAI-compatible transport. Per-brand data (base URL, default model, identity
// headers, provider_type reflection) comes from the wire brand catalog.
func registerProvidersFromDB(registry *providers.Registry, provStore store.ProviderStore, secretStore store.ConfigSecretsStore, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore, cfg *config.Config, modelReg providers.ModelRegistry, providerLanes *scheduler.LaneManager) {
	dbProviders, err := provStore.ListAllProviders(context.Background())
	if err != nil {
		slog.Warn("failed to load providers from DB", "error", err)
		return
	}
	for _, p := range dbProviders {
		if !p.Enabled {
			continue
		}
		if !registerDBProvider(registry, p, secretStore, gatewayAddr, gatewayToken, mcpStore, cfg, modelReg, provStore, providerLanes) {
			continue
		}
		slog.Info("registered provider from DB", "name", p.Name)
	}
}

// registerDBProvider builds and registers one row. It reports whether the
// provider was registered; every skip path has already been logged.
func registerDBProvider(registry *providers.Registry, p store.LLMProviderData, secretStore store.ConfigSecretsStore, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore, cfg *config.Config, modelReg providers.ModelRegistry, provStore store.ProviderStore, providerLanes *scheduler.LaneManager) bool {
	desc, ok := wire.Lookup(wire.API(p.WireAPI))
	if !ok {
		slog.Error("provider.wire_api.unknown",
			"provider", p.Name,
			"wire_api", p.WireAPI,
			"hint", "fix the llm_providers.wire_api value or upgrade this build; the provider is skipped (no default transport is assumed)")
		return false
	}

	// Fall back to config/env api_base when the row has none set.
	if p.APIBase == "" && cfg != nil {
		if base := cfg.Providers.APIBaseForType(p.ProviderType); base != "" {
			p.APIBase = base
			slog.Info("provider api_base inherited from config", "name", p.Name, "api_base", base)
		}
	}

	// Keyless, service-account and delegated transports register without a
	// credential; static-key and OAuth rows need one present (the OAuth token
	// itself lives in the credential store, not in the row).
	if desc.RequiresAPIKey && p.APIKey == "" {
		return false
	}

	build := wire.Config{
		API:             desc.API,
		Source:          wire.SourceDB,
		Name:            p.Name,
		ProviderType:    p.ProviderType,
		APIKey:          p.APIKey,
		BaseURL:         p.APIBase,
		Timeout:         wire.TimeoutFromSettings(p.Settings),
		ConcurrencyGate: providerConcurrencyGate(providerLanes, p.Name, wire.MaxInFlightFromSettings(p.Settings)),
		Thinking:        store.ParseThinkingEnabled(p.Settings),
		OllamaNumCtx:    resolveOllamaNumCtx(&p),
	}
	if provStore != nil {
		// Both catalogue reads are tenant-scoped: ListModels fails closed without a
		// tenant (ErrTenantRequired) and ListQuirks falls back to bundled rows only.
		// A bare context therefore dropped per-model compat and tenant quirk
		// overrides at registration.
		pctx := providerTenantContext(p.TenantID)
		build.Quirks = loadQuirks(pctx, provStore, p.WireAPI)
		build.ModelCompat = loadModelCompat(pctx, provStore, p.ID)
	}

	switch desc.API {
	case wire.OpenAICodexResponses:
		if secretStore == nil && provStore == nil {
			slog.Error("provider.register.failed", "provider", p.Name, "wire_api", p.WireAPI, "error", "no credential store available for OAuth")
			return false
		}
		build.TokenSource = oauth.NewDBTokenSource(provStore, secretStore, p.Name).WithTenantID(p.TenantID)
		if s := store.ParseChatGPTOAuthProviderSettings(p.Settings); s != nil && s.CodexPool != nil {
			build.Routing = &providers.CodexRoutingDefaults{
				Strategy:           s.CodexPool.Strategy,
				ExtraProviderNames: s.CodexPool.ExtraProviderNames,
			}
		}
	case wire.AnthropicMessages:
		build.Registry = modelReg
	case wire.GoogleVertex:
		vs := store.ParseVertexProviderSettings(p.Settings)
		if vs == nil {
			slog.Warn("vertex: missing project_id/region in settings, skipping", "name", p.Name)
			return false
		}
		build.Vertex = &wire.VertexSettings{ProjectID: vs.ProjectID, Region: vs.Region, Model: vs.Model}
	case wire.CLIDelegated:
		cli, ok := cliSettingsFromRow(p, gatewayAddr, gatewayToken, mcpStore, cfg)
		if !ok {
			return false
		}
		build.CLI = cli
	}

	prov, err := wire.Build(build)
	if err != nil {
		slog.Error("provider.register.failed", "provider", p.Name, "wire_api", p.WireAPI, "error", err)
		return false
	}
	registry.RegisterForTenant(p.TenantID, prov)
	return true
}

// providerTenantContext scopes provider catalogue reads to the row's owning
// tenant. llm_providers.tenant_id is NOT NULL (migrations/000027), so Nil only
// occurs on synthetic rows; those fall back to the master tenant so the read
// stays fail-closed instead of erroring out.
func providerTenantContext(tenantID uuid.UUID) context.Context {
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	return store.WithTenantID(context.Background(), tenantID)
}

// loadQuirks reads the operator's declared quirk rows for a wire API and
// converts them to the resolver's plain-value form. A store failure is a warning
// (the bundled seeds still apply), never a registration failure.
func loadQuirks(ctx context.Context, provStore store.ProviderStore, wireAPI string) []compat.Quirk {
	rows, err := provStore.ListQuirks(ctx, wireAPI)
	if err != nil {
		slog.Warn("provider.quirks.load_failed", "wire_api", wireAPI, "error", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	out := make([]compat.Quirk, 0, len(rows))
	for _, row := range rows {
		q := compat.Quirk{
			WireAPI: row.WireAPI,
			Compat:  row.Compat,
			Source:  row.Source,
			Enabled: row.Enabled,
			Note:    derefQuirkString(row.Note),
		}
		if row.EndpointFamily != nil {
			q.EndpointFamily = *row.EndpointFamily
		}
		if row.ModelPattern != nil {
			q.ModelPattern = *row.ModelPattern
		}
		// Invalid operator data is skipped loudly rather than reaching a request.
		if err := compat.Validate(q.Compat); err != nil {
			slog.Warn("provider.quirks.invalid", "wire_api", row.WireAPI, "error", err)
			continue
		}
		out = append(out, q)
	}
	return out
}

// loadModelCompat reads a provider's catalogue rows and returns the non-empty
// llm_models.compat fragments by model id, so the wire builder resolves each
// model's compat once at registration.
func loadModelCompat(ctx context.Context, provStore store.ProviderStore, providerID uuid.UUID) map[string]json.RawMessage {
	rows, err := provStore.ListModels(ctx, providerID)
	if err != nil {
		slog.Warn("provider.models.load_failed", "provider_id", providerID, "error", err)
		return nil
	}
	out := make(map[string]json.RawMessage, len(rows))
	for _, row := range rows {
		if len(row.Compat) == 0 {
			continue
		}
		if err := compat.Validate(row.Compat); err != nil {
			slog.Warn("provider.models.invalid_compat", "provider_id", providerID, "model", row.ModelID, "error", err)
			continue
		}
		out[row.ModelID] = row.Compat
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func derefQuirkString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// resolveOllamaNumCtx returns the operator-configured num_ctx for an Ollama
// provider, or nil to let the provider resolve it per model at request time.
//
// Only the explicit settings JSONB override is honoured here. Probing /api/show
// at startup cannot work: the model an agent will use is not known until it
// sends a request, so the probe had to guess a model name, and a wrong guess
// resolved to nothing. OllamaProvider.resolveNumCtx does the lookup against the
// real model instead, and caches it.
func resolveOllamaNumCtx(p *store.LLMProviderData) *int {
	if s := store.ParseOllamaSettings(p.Settings); s != nil {
		return s.NumCtx
	}
	return nil
}

// cliSettingsFromRow assembles the subprocess configuration for a cli-delegated
// row. exec_path is authoritative; api_base is the one-release dual-read
// fallback for rows written before phase 1. Which subprocess contract applies
// is brand data (claude_cli vs acp), not a switch here.
func cliSettingsFromRow(p store.LLMProviderData, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore, cfg *config.Config) (*wire.CLISettings, bool) {
	brand, _ := wire.BrandFor(p.ProviderType)
	switch brand.CLIKind {
	case wire.CLIKindClaude:
		return claudeCLISettingsFromRow(p, gatewayAddr, gatewayToken, mcpStore, cfg)
	case wire.CLIKindACP:
		return acpSettingsFromRow(p, cfg)
	default:
		slog.Error("provider.register.failed", "provider", p.Name, "wire_api", p.WireAPI,
			"error", "cli-delegated row has no known subprocess brand; expected "+wire.CLIKindClaude+" or "+wire.CLIKindACP)
		return nil, false
	}
}

// claudeCLISettingsFromRow builds the Claude CLI subprocess configuration.
func claudeCLISettingsFromRow(p store.LLMProviderData, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore, cfg *config.Config) (*wire.CLISettings, bool) {
	cliPath := cliExecPath(p)
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
		return nil, false
	}
	cli := &wire.CLISettings{
		Name:          p.Name,
		Path:          cliPath,
		SecurityHooks: true,
		DenyPatterns:  configuredShellDenyPatterns(cfg),
	}
	if gatewayAddr != "" {
		mcpData := providers.BuildCLIMCPConfigData(nil, gatewayAddr, gatewayToken)
		mcpData.AgentMCPLookup = buildMCPServerLookup(mcpStore)
		cli.MCP = mcpData
	}
	return cli, true
}

// acpSettingsFromRow builds the ACP subprocess configuration from a DB row.
func acpSettingsFromRow(p store.LLMProviderData, cfg *config.Config) (*wire.CLISettings, bool) {
	return acpCLISettings(p, tools.ResolveDenyPatterns(configuredShellDenyGroups(cfg)))
}

// acpCLISettings validates a row's executable and assembles its ACP subprocess
// configuration. The path allowlist matches the create/update validator: a bare
// known agent name or an absolute path.
func acpCLISettings(p store.LLMProviderData, denyPatterns []*regexp.Regexp) (*wire.CLISettings, bool) {
	binary := cliExecPath(p)
	if binary == "" {
		slog.Warn("acp: no binary specified in DB provider", "name", p.Name)
		return nil, false
	}
	if binary != "claude" && binary != "codex" && binary != "gemini" && !filepath.IsAbs(binary) {
		slog.Warn("security.acp: invalid binary path from DB", "path", binary)
		return nil, false
	}
	if _, err := exec.LookPath(binary); err != nil {
		slog.Warn("acp: binary not found, skipping", "binary", binary, "error", err)
		return nil, false
	}
	settings := parsedACPSettings(p)
	workDir := settings.WorkDir
	if workDir == "" {
		workDir = defaultACPWorkDir()
	}
	return &wire.CLISettings{
		Name:         p.Name,
		Path:         binary,
		Model:        p.Name,
		Args:         settings.Args,
		WorkDir:      workDir,
		IdleTTL:      settings.idleTTL(),
		PermMode:     settings.PermMode,
		DenyPatterns: denyPatterns,
	}, true
}

// cliExecPath resolves a cli-delegated row's executable: exec_path is
// authoritative, api_base is the fallback for one release.
func cliExecPath(p store.LLMProviderData) string {
	if p.ExecPath != "" {
		return p.ExecPath
	}
	return p.APIBase
}

// registerClaudeCLIFromConfig registers the Claude CLI provider declared in the
// config file (claude_cli wire API, cli-delegated auth).
func registerClaudeCLIFromConfig(registry *providers.Registry, cfg *config.Config) {
	if cfg == nil || cfg.Providers.ClaudeCLI.CLIPath == "" {
		return
	}
	gatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
	cli := &wire.CLISettings{
		Path:          cfg.Providers.ClaudeCLI.CLIPath,
		Model:         cfg.Providers.ClaudeCLI.Model,
		WorkDir:       cfg.Providers.ClaudeCLI.BaseWorkDir,
		PermMode:      cfg.Providers.ClaudeCLI.PermMode,
		SecurityHooks: true,
		DenyPatterns:  configuredShellDenyPatterns(cfg),
		MCP:           providers.BuildCLIMCPConfigData(cfg.Tools.McpServers, gatewayAddr, cfg.Gateway.Token),
	}
	registerConfigProvider(registry, wire.Config{
		API:          wire.CLIDelegated,
		Source:       wire.SourceConfig,
		Name:         "claude-cli",
		ProviderType: store.ProviderClaudeCLI,
		CLI:          cli,
	})
}

// registerClaudeCLIFromDB registers a Claude CLI provider from a DB provider row.
func registerClaudeCLIFromDB(registry *providers.Registry, p store.LLMProviderData, gatewayAddr, gatewayToken string, mcpStore store.MCPServerStore, cfg *config.Config) bool {
	cli, ok := claudeCLISettingsFromRow(p, gatewayAddr, gatewayToken, mcpStore, cfg)
	if !ok {
		return false
	}
	prov, err := wire.Build(wire.Config{
		API:          wire.CLIDelegated,
		Source:       wire.SourceDB,
		Name:         p.Name,
		ProviderType: p.ProviderType,
		CLI:          cli,
	})
	if err != nil {
		slog.Error("provider.register.failed", "provider", p.Name, "wire_api", store.WireAPICLIDelegated, "error", err)
		return false
	}
	registry.RegisterForTenant(p.TenantID, prov)
	slog.Info("registered provider from DB", "name", p.Name)
	return true
}

// registerACPFromConfig registers an ACP provider from config file settings.
func registerACPFromConfig(registry *providers.Registry, cfg config.ACPConfig, shellDenyGroups map[string]bool) {
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
	registerConfigProvider(registry, wire.Config{
		API:          wire.CLIDelegated,
		Source:       wire.SourceConfig,
		Name:         "acp",
		ProviderType: store.ProviderACP,
		CLI: &wire.CLISettings{
			Path:         cfg.Binary,
			Model:        cfg.Model,
			Args:         cfg.Args,
			WorkDir:      workDir,
			IdleTTL:      idleTTL,
			PermMode:     cfg.PermMode,
			DenyPatterns: tools.ResolveDenyPatterns(shellDenyGroups),
		},
	})
}

// registerACPFromDB registers an ACP provider from a DB provider row.
func registerACPFromDB(registry *providers.Registry, p store.LLMProviderData, shellDenyGroups map[string]bool) {
	cli, ok := acpCLISettings(p, tools.ResolveDenyPatterns(shellDenyGroups))
	if !ok {
		return
	}
	prov, err := wire.Build(wire.Config{
		API:          wire.CLIDelegated,
		Source:       wire.SourceDB,
		Name:         p.Name,
		ProviderType: p.ProviderType,
		CLI:          cli,
	})
	if err != nil {
		slog.Error("provider.register.failed", "provider", p.Name, "wire_api", store.WireAPICLIDelegated, "error", err)
		return
	}
	registry.RegisterForTenant(p.TenantID, prov)
	slog.Info("registered provider from DB", "name", p.Name, "type", "acp")
}

// acpRowSettings is the ACP-specific part of a provider row's settings JSONB.
type acpRowSettings struct {
	Args     []string `json:"args"`
	IdleTTL  string   `json:"idle_ttl"`
	PermMode string   `json:"perm_mode"`
	WorkDir  string   `json:"work_dir"`
}

// idleTTL parses the configured idle TTL, defaulting to five minutes.
func (s acpRowSettings) idleTTL() time.Duration {
	if s.IdleTTL != "" {
		if d, err := time.ParseDuration(s.IdleTTL); err == nil {
			return d
		}
	}
	return 5 * time.Minute
}

// parsedACPSettings decodes the ACP settings of a row, logging and defaulting on
// malformed JSON.
func parsedACPSettings(p store.LLMProviderData) acpRowSettings {
	var settings acpRowSettings
	if p.Settings != nil {
		if err := json.Unmarshal(p.Settings, &settings); err != nil {
			slog.Warn("acp: invalid settings JSON, using defaults", "name", p.Name, "error", err)
		}
	}
	return settings
}

func configuredShellDenyGroups(cfg *config.Config) map[string]bool {
	if cfg == nil {
		return nil
	}
	return cfg.ShellDenyGroupsSnapshot()
}

func configuredShellDenyPatterns(cfg *config.Config) []*regexp.Regexp {
	return tools.ResolveDenyPatterns(configuredShellDenyGroups(cfg))
}

// defaultACPWorkDir returns the default workspace directory for ACP agents.
func defaultACPWorkDir() string {
	return filepath.Join(config.ResolvedDataDirFromEnv(), "acp-workspaces")
}
