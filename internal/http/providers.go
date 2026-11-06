package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/catalog"
	"github.com/nextlevelbuilder/goclaw/internal/providers/discovery"
	"github.com/nextlevelbuilder/goclaw/internal/providers/wire"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ProvidersHandler handles LLM provider CRUD endpoints.
type ProvidersHandler struct {
	store           store.ProviderStore
	secretStore     store.ConfigSecretsStore
	providerReg     *providers.Registry
	gatewayAddr     string                           // for injecting MCP bridge into Claude CLI providers
	mcpLookup       providers.MCPServerLookup        // optional: resolves per-agent MCP servers
	shellDenyGroups func() map[string]bool           // optional: current global shell deny-group overrides
	apiBaseFallback func(providerType string) string // optional: config/env fallback for api_base
	cliMu           sync.Mutex                       // serializes Claude CLI provider create to prevent duplicates
	msgBus          *bus.MessageBus
	sysConfigStore  store.SystemConfigStore
	tracingStore    store.TracingStore      // optional: for provider-scoped pool activity
	agents          store.AgentCRUDStore    // optional: for provider pool activity agent lookup
	modelReg        providers.ModelRegistry // optional: forward-compat model resolver for Anthropic
	usageCaps       *usagecaps.Service
	// modelCatalog owns the llm_models catalogue: bundled seeding, discovery
	// refresh, fingerprint cache and merging.
	modelCatalog *catalog.Service
}

// NewProvidersHandler creates a handler for provider management endpoints.
func NewProvidersHandler(s store.ProviderStore, secretStore store.ConfigSecretsStore, providerReg *providers.Registry, gatewayAddr string) *ProvidersHandler {
	return &ProvidersHandler{
		store:        s,
		secretStore:  secretStore,
		providerReg:  providerReg,
		gatewayAddr:  gatewayAddr,
		modelCatalog: catalog.NewService(s, discovery.NewRegistry(validateProviderURL)),
	}
}

// SetMessageBus sets the message bus for audit event broadcasting.
// Must be called before serving requests (not thread-safe).
func (h *ProvidersHandler) SetMessageBus(msgBus *bus.MessageBus) {
	h.msgBus = msgBus
}

// SetSystemConfigStore sets the system config store for embedding status checks.
func (h *ProvidersHandler) SetSystemConfigStore(s store.SystemConfigStore) {
	h.sysConfigStore = s
}

// SetMCPServerLookup sets the per-agent MCP server lookup for Claude CLI providers.
// Must be called before serving requests (not thread-safe).
func (h *ProvidersHandler) SetMCPServerLookup(lookup providers.MCPServerLookup) {
	h.mcpLookup = lookup
}

// SetShellDenyGroupsSource sets the current global shell deny-group source for
// runtime provider registration. Must be called before serving requests.
func (h *ProvidersHandler) SetShellDenyGroupsSource(fn func() map[string]bool) {
	h.shellDenyGroups = fn
}

// SetAPIBaseFallback sets a function that returns config/env api_base by provider type.
// Used as fallback when DB providers have no api_base set.
func (h *ProvidersHandler) SetAPIBaseFallback(fn func(providerType string) string) {
	h.apiBaseFallback = fn
}

// SetTracingStore sets the tracing store for provider-scoped pool activity.
func (h *ProvidersHandler) SetTracingStore(ts store.TracingStore) {
	h.tracingStore = ts
}

// SetAgentStore sets the agent store for provider pool activity agent lookup.
func (h *ProvidersHandler) SetAgentStore(as store.AgentCRUDStore) {
	h.agents = as
}

// SetModelRegistry sets the forward-compat model registry used by Anthropic providers
// for model alias resolution and token counting. Must be called before serving requests.
func (h *ProvidersHandler) SetModelRegistry(r providers.ModelRegistry) {
	h.modelReg = r
}

func (h *ProvidersHandler) SetUsageCapService(s *usagecaps.Service) {
	h.usageCaps = s
}

func (h *ProvidersHandler) currentShellDenyPatterns() []*regexp.Regexp {
	if h.shellDenyGroups == nil {
		return tools.DefaultDenyPatterns()
	}
	return tools.ResolveDenyPatterns(h.shellDenyGroups())
}

// resolveAPIBase returns the provider's api_base, falling back to config/env if empty.
// For Ollama/OllamaCloud providers, applies a safety-net normalization: if the stored
// value is missing the /v1 suffix (pre-existing record before write-time normalization),
// the suffix is appended so all downstream call sites receive a ready-to-use URL.
func (h *ProvidersHandler) resolveAPIBase(p *store.LLMProviderData) string {
	base := ""
	if p.APIBase != "" {
		base = p.APIBase
	} else if h.apiBaseFallback != nil {
		base = h.apiBaseFallback(p.ProviderType)
	}
	// Safety net: normalize Ollama URLs missing /v1 (pre-existing DB records).
	if base != "" && (p.ProviderType == store.ProviderOllama || p.ProviderType == store.ProviderOllamaCloud) {
		base = strings.TrimRight(base, "/")
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
	}
	return base
}

// emitProviderCacheInvalidate broadcasts a provider cache invalidation event.
// Subscribers (e.g. ACP re-registration in gateway_managed.go) react to reload from DB.
func (h *ProvidersHandler) emitProviderCacheInvalidate(ctx context.Context, tenantID uuid.UUID, name string) bool {
	if h.msgBus == nil {
		return false
	}
	tenantID = providerCacheTenantID(ctx, tenantID)
	h.msgBus.Broadcast(bus.Event{
		Name:     protocol.EventCacheInvalidate,
		TenantID: tenantID,
		Payload:  bus.CacheInvalidatePayload{Kind: bus.CacheKindProvider, Key: name, TenantID: tenantID},
	})
	return true
}

func providerCacheTenantID(ctx context.Context, tenantID uuid.UUID) uuid.UUID {
	if tenantID != uuid.Nil {
		return tenantID
	}
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		return tid
	}
	return store.MasterTenantID
}

// RegisterRoutes registers all provider management routes on the given mux.
func (h *ProvidersHandler) RegisterRoutes(mux *http.ServeMux) {
	// Provider CRUD — reads are needed by /setup for browser-paired Operators
	// (issue #1075), so GET routes use read-level auth (GET→Viewer) while
	// mutations stay Admin-only.
	mux.HandleFunc("GET /v1/providers", h.readAuth(h.handleListProviders))
	mux.HandleFunc("POST /v1/providers", h.auth(h.handleCreateProvider))
	mux.HandleFunc("GET /v1/providers/{id}", h.readAuth(h.handleGetProvider))
	mux.HandleFunc("PUT /v1/providers/{id}", h.auth(h.handleUpdateProvider))
	mux.HandleFunc("DELETE /v1/providers/{id}", h.auth(h.handleDeleteProvider))

	// Model listing (proxied to upstream provider API)
	mux.HandleFunc("GET /v1/providers/{id}/models", h.readAuth(h.handleListProviderModels))
	// Provider quirks listing (read-only)
	mux.HandleFunc("GET /v1/providers/quirks", h.readAuth(h.handleListQuirks))
	// Capability DTO: the single shape every UI surface builds its provider/model
	// picker from (provider declaration + per-model capabilities, no transport
	// detail).
	mux.HandleFunc("GET /v1/providers/capabilities", h.readAuth(h.handleListProviderCapabilities))


	// Gateway-scoped model catalogue (OpenAI shape), tenant-scoped and read-only.
	// {model...} keeps multi-segment vendor ids addressable.
	mux.HandleFunc("GET /v1/models", h.readAuth(h.handleListModels))
	mux.HandleFunc("GET /v1/models/{provider}/{model...}", h.readAuth(h.handleGetModel))

	// Provider + model verification (pre-flight check) — mutating actions, Admin.
	mux.HandleFunc("POST /v1/providers/{id}/reconnect", h.auth(h.handleReconnectProvider))
	mux.HandleFunc("POST /v1/providers/{id}/verify", h.auth(h.handleVerifyProvider))
	mux.HandleFunc("POST /v1/providers/{id}/verify-embedding", h.auth(h.handleVerifyEmbedding))

	// Provider-scoped Codex pool activity monitor (read-only status)
	mux.HandleFunc("GET /v1/providers/{id}/codex-pool-activity", h.readAuth(h.handleProviderCodexPoolActivity))

	// Provider health (durable cooldown state + manual reset / active probe).
	// Registered without a method prefix so the resource keeps one canonical
	// path: the handler dispatches per method and applies each method's own auth
	// level (GET → Viewer, POST → Admin).
	mux.HandleFunc("/v1/providers/{id}/health", h.handleProviderHealthRoute)
	// Provider usage detail: latency percentiles + recent failed calls (Viewer).
	mux.HandleFunc("GET /v1/providers/{id}/usage-detail", h.readAuth(h.handleProviderUsageDetail))

	// Embedding system status (read-only)
	mux.HandleFunc("GET /v1/embedding/status", h.readAuth(h.handleEmbeddingStatus))

	// Claude CLI auth status (global — not per-provider; read-only status)
	mux.HandleFunc("GET /v1/providers/claude-cli/auth-status", h.readAuth(h.handleClaudeCLIAuthStatus))
}

// auth gates provider mutations at Admin.
func (h *ProvidersHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

// readAuth gates read-only provider endpoints at the method-derived minimum
// (GET→Viewer), so browser-paired Operators can complete /setup (issue #1075).
// Responses already mask API keys, and provider queries stay tenant-scoped.
func (h *ProvidersHandler) readAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// maskAPIKey replaces non-empty API keys with "***".
func maskAPIKey(p *store.LLMProviderData) {
	if p.APIKey != "" {
		p.APIKey = "***"
	}
}

type providerRuntimeRegistrationStatus string

const (
	providerRuntimeRegistered        providerRuntimeRegistrationStatus = "registered"
	providerRuntimeDisabled          providerRuntimeRegistrationStatus = "disabled"
	providerRuntimeSkipped           providerRuntimeRegistrationStatus = "skipped"
	providerRuntimeMissingCredential providerRuntimeRegistrationStatus = "missing_credential"
	providerRuntimeInvalidConfig     providerRuntimeRegistrationStatus = "invalid_config"
)

// registerInMemory adds (or replaces) a provider in the in-memory registry
// so it's immediately usable for verify/chat without a gateway restart.
func (h *ProvidersHandler) registerInMemory(p *store.LLMProviderData) providerRuntimeRegistrationStatus {
	if h.providerReg == nil || !p.Enabled {
		if p.Enabled {
			return providerRuntimeSkipped
		}
		return providerRuntimeDisabled
	}
	// ACP agents don't need an API key — skip in-memory registration
	// (ACP providers are registered via gateway_providers.go on startup or restart)
	if p.ProviderType == store.ProviderACP {
		return providerRuntimeSkipped
	}
	// Claude CLI doesn't need an API key — register immediately
	if p.ProviderType == store.ProviderClaudeCLI {
		cliPath := p.APIBase // reuse APIBase field for CLI path
		if cliPath == "" {
			cliPath = "claude"
		}
		// Validate: only accept "claude" or absolute path (mirrors startup path in cmd/gateway_providers.go).
		// Prevents DB-poisoning attacks where a relative path resolves against CWD.
		if cliPath != "claude" && !filepath.IsAbs(cliPath) {
			slog.Warn("security.claude_cli: invalid path, using default", "path", cliPath, "provider", p.Name)
			cliPath = "claude"
		}
		if _, err := exec.LookPath(cliPath); err != nil {
			slog.Warn("claude-cli: binary not found, skipping in-memory registration", "path", cliPath, "provider", p.Name, "error", err)
			return providerRuntimeInvalidConfig
		}
		cliOpts := []providers.ClaudeCLIOption{
			providers.WithClaudeCLIName(p.Name),
			providers.WithClaudeCLISecurityHooks("", true, h.currentShellDenyPatterns()),
		}
		if h.gatewayAddr != "" {
			mcpData := providers.BuildCLIMCPConfigData(nil, h.gatewayAddr, pkgGatewayToken)
			mcpData.AgentMCPLookup = h.mcpLookup
			cliOpts = append(cliOpts, providers.WithClaudeCLIMCPConfigData(mcpData))
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewClaudeCLIProvider(cliPath, cliOpts...))
		return providerRuntimeRegistered
	}
	// Ollama doesn't need an API key — handle before the key guard (same as startup).
	// In Docker, swap localhost → host.docker.internal so the container can reach the host.
	if p.ProviderType == store.ProviderOllama {
		host := p.APIBase
		if host == "" {
			host = "http://localhost:11434"
		}
		dockerHost := config.DockerLocalhost(host)
		numCtx := h.resolveOllamaNumCtx(p, dockerHost, "")
		prov := providers.NewOllamaProvider(p.Name, dockerHost, "llama3.3", numCtx, nil).
			WithThinkingEnabled(store.ParseThinkingEnabled(p.Settings))
		h.providerReg.RegisterForTenant(p.TenantID, prov)
		return providerRuntimeRegistered
	}
	// Vertex supports ADC (empty api_key) — handle before the generic key guard.
	if p.ProviderType == store.ProviderVertex {
		vsettings := store.ParseVertexProviderSettings(p.Settings)
		if vsettings == nil {
			slog.Warn("vertex: missing project_id/region in settings, cannot register", "name", p.Name)
			return providerRuntimeInvalidConfig
		}
		vcfg := providers.VertexConfig{
			Name:            p.Name,
			CredentialsJSON: p.APIKey,
			ProjectID:       vsettings.ProjectID,
			Region:          vsettings.Region,
			DefaultModel:    vsettings.Model,
			APIBaseOverride: p.APIBase,
		}
		prov, err := providers.NewVertexProviderWithTimeout(vcfg)
		if err != nil {
			slog.Warn("vertex: register in-memory failed", "name", p.Name, "error", err)
			return providerRuntimeInvalidConfig
		}
		h.providerReg.RegisterForTenant(p.TenantID, prov)
		return providerRuntimeRegistered
	}
	if p.APIKey == "" {
		return providerRuntimeMissingCredential
	}
	apiBase := h.resolveAPIBase(p)
	switch p.ProviderType {
	case store.ProviderAPIRoute:
		if apiBase == "" {
			apiBase = store.APIRouteDefaultAPIBase
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, apiBase, store.APIRouteDefaultModel).WithProviderType(p.ProviderType))
	case store.ProviderChatGPTOAuth:
		ts := oauth.NewDBTokenSource(h.store, h.secretStore, p.Name).WithTenantID(p.TenantID)
		codex := providers.NewCodexProvider(p.Name, ts, apiBase, "")
		if oauthSettings := store.ParseChatGPTOAuthProviderSettings(p.Settings); oauthSettings != nil {
			codex.WithRoutingDefaults(oauthSettings.CodexPool.Strategy, oauthSettings.CodexPool.ExtraProviderNames)
		}
		h.providerReg.RegisterForTenant(p.TenantID, codex)
	case store.ProviderAnthropicNative:
		anthOpts := []providers.AnthropicOption{
			providers.WithAnthropicName(p.Name),
			providers.WithAnthropicBaseURL(apiBase),
		}
		if h.modelReg != nil {
			anthOpts = append(anthOpts, providers.WithAnthropicRegistry(h.modelReg))
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewAnthropicProvider(p.APIKey, anthOpts...))
	case store.ProviderDashScope:
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewDashScopeProvider(p.Name, p.APIKey, apiBase, ""))
	case store.ProviderBailian:
		base := apiBase
		if base == "" {
			base = "https://coding-intl.dashscope.aliyuncs.com/v1"
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus").
			WithProviderType(p.ProviderType))
	case store.ProviderZai:
		base := apiBase
		if base == "" {
			base = store.ZaiDefaultAPIBase
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.ZaiDefaultModel))
	case store.ProviderZaiCoding:
		base := apiBase
		if base == "" {
			base = store.ZaiCodingDefaultAPIBase
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.ZaiDefaultModel))
	case store.ProviderNovita:
		base := apiBase
		if base == "" {
			base = store.NovitaDefaultAPIBase
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.NovitaDefaultModel))
	case store.ProviderKimiCoding:
		// Moonshot Kimi Coding requires a fixed User-Agent on every request.
		base := apiBase
		if base == "" {
			base = store.KimiCodingDefaultAPIBase
		}
		prov := providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.KimiCodingDefaultModel)
		prov.WithProviderType(p.ProviderType)
		prov.WithExtraHeaders(map[string]string{
			"User-Agent": store.KimiCodingRequiredUserAgent,
		})
		h.providerReg.RegisterForTenant(p.TenantID, prov)
	case store.ProviderAIMLAPI:
		prov := providers.NewAIMLAPIProvider(p.Name, p.APIKey, apiBase)
		prov.WithProviderType(p.ProviderType)
		h.providerReg.RegisterForTenant(p.TenantID, prov)
	case store.ProviderOllamaCloud:
		base := apiBase
		if base == "" {
			base = "https://ollama.com"
		}
		numCtx := h.resolveOllamaNumCtx(p, base, p.APIKey)
		prov := providers.NewOllamaProvider(p.Name, base, "llama3.3", numCtx, nil).
			WithThinkingEnabled(store.ParseThinkingEnabled(p.Settings))
		h.providerReg.RegisterForTenant(p.TenantID, prov)
	default:
		base, model, brand, hasBrand := openAIBrandDefaults(p.ProviderType, apiBase)
		prov := providers.NewOpenAIProvider(p.Name, p.APIKey, base, model)
		if hasBrand {
			// Same data the boot path gets from wire.Build (cmd/gateway_providers.go):
			// without it a provider created here behaves differently until the next
			// gateway restart — that is how an OpenCode row ended up calling
			// api.openai.com with no x-opencode-session (2026-09-29).
			prov.WithExtraHeaders(brand.ExtraHeaders).WithSessionHeader(brand.SessionHeader)
		}
		prov.WithThinkingEnabled(store.ParseThinkingEnabled(p.Settings))
		h.providerReg.RegisterForTenant(p.TenantID, prov)
	}
	return providerRuntimeRegistered
}

// resolveOllamaNumCtx returns the num_ctx to pass to NewOllamaProvider. Priority:
//  1. User-configured num_ctx from provider settings JSONB.
//  2. Value queried from Ollama /api/show.
//  3. OllamaDefaultNumCtx (131072) — never nil, so Ollama always uses a large context window.
func (h *ProvidersHandler) resolveOllamaNumCtx(p *store.LLMProviderData, apiBase, apiKey string) *int {
	slog.Debug("ollama.startup: resolveOllamaNumCtx called", "provider", p.Name, "api_base", apiBase)
	if s := store.ParseOllamaSettings(p.Settings); s != nil {
		slog.Info("ollama.startup: using num_ctx from provider settings", "provider", p.Name, "num_ctx", *s.NumCtx)
		return s.NumCtx
	}
	slog.Debug("ollama.startup: no settings num_ctx, querying /api/show", "provider", p.Name)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	numCtx := providers.FetchOllamaModelContext(ctx, apiBase, "llama3.3", apiKey)
	slog.Info("ollama.startup: applying num_ctx from /api/show (or default fallback)", "provider", p.Name, "num_ctx", numCtx)
	return &numCtx
}

func openAIProviderDefaults(providerType, apiBase string) (string, string) {
	switch providerType {
	case store.ProviderMiniMax:
		if apiBase == "" {
			apiBase = store.MiniMaxDefaultAPIBase
		}
		return apiBase, store.MiniMaxDefaultModel
	case store.ProviderAtlasCloud:
		if apiBase == "" {
			apiBase = store.AtlasCloudDefaultAPIBase
		}
		return apiBase, store.AtlasCloudDefaultModel
	case store.ProviderRequesty:
		if apiBase == "" {
			apiBase = store.RequestyDefaultAPIBase
		}
		return apiBase, store.RequestyDefaultModel
	default:
		return apiBase, ""
	}
}

// openAIBrandDefaults resolves the OpenAI-compatible construction inputs for a
// provider type this handler has no dedicated branch for.
//
// The legacy per-type table (openAIProviderDefaults) knows only a couple of
// vendors and falls back to the row's api_base with an empty model; the wire
// catalog is the single source of truth for every shipped brand, including its
// base URL, default model, identity headers and per-conversation session header.
// Merging the two keeps a provider created here identical to the same row built
// by the boot path (wire.Build).
func openAIBrandDefaults(providerType, apiBase string) (base, model string, brand wire.Brand, ok bool) {
	base, model = openAIProviderDefaults(providerType, apiBase)
	brand, ok = wire.BrandFor(providerType)
	if !ok {
		return base, model, brand, false
	}
	if base == "" {
		base = brand.BaseURL
	}
	if model == "" {
		model = brand.Model
	}
	return base, model, brand, true
}

// normalizeOllamaAPIBase normalizes the api_base stored for Ollama providers.
// The /v1 suffix is added at write time for backward compatibility; NewOllamaProvider
// strips it automatically before constructing the native Ollama client URL.
func normalizeOllamaAPIBase(p *store.LLMProviderData) {
	if p.ProviderType != store.ProviderOllama && p.ProviderType != store.ProviderOllamaCloud {
		return
	}
	if p.APIBase == "" {
		return
	}
	p.APIBase = strings.TrimRight(p.APIBase, "/")
	if !strings.HasSuffix(p.APIBase, "/v1") {
		p.APIBase += "/v1"
	}
}

// localURLProviderTypes are provider types that legitimately run on localhost.
// They are restricted to an explicit localhost allowlist
// rather than skipping SSRF validation entirely.
// ACP is intentionally excluded: its api_base carries an executable command/path,
// not a URL (see issue #1481).
var localURLProviderTypes = map[string]bool{
	store.ProviderOllama: true,
	// Ollama Cloud speaks the same native API; the bearer token is the only
	// difference, and a self-hosted Ollama behind a token is a legitimate
	// api_base for it (the localhost allowlist still applies).
	store.ProviderOllamaCloud: true,
}

// allowedLocalHosts are the only hosts permitted for local provider types.
// Explicit allowlist (not blocklist) to prevent new internal addresses from
// slipping through (e.g. 169.254.169.254 via ollama base URL).
var allowedLocalHosts = []string{"localhost", "127.0.0.1", "::1", "host.docker.internal"}

// dnsResolverFn resolves hostnames to IPs. Replaceable in tests.
var dnsResolverFn = net.LookupHost

// allowPrivateProviderURLsFn reports whether the operator has opted in to
// permitting private / loopback / link-local / internal-hostname provider base
// URLs via GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS. Evaluated once at first call so
// tests can override the variable before that happens.
var allowPrivateProviderURLsFn = sync.OnceValue(func() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS")))
	return v == "1" || v == "true" || v == "yes"
})

// ollamaAllowedHostsFn returns the operator-configured extra hosts permitted
// for local provider types (ollama, acp) via GOCLAW_OLLAMA_ALLOWED_HOSTS
// (comma-separated hostnames/IPs, e.g. "192.168.3.31,ollama.lan"). These are
// added on top of allowedLocalHosts, allowing LAN-hosted Ollama servers.
// Evaluated once at first call so tests can override the variable before that happens.
var ollamaAllowedHostsFn = sync.OnceValue(func() []string {
	raw := os.Getenv("GOCLAW_OLLAMA_ALLOWED_HOSTS")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	hosts := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			hosts = append(hosts, p)
		}
	}
	return hosts
})

// validateProviderURL rejects provider base URLs pointing to internal/private networks.
// Defense-in-depth: prevents SSRF when providers are later used for API calls.
//
// Logic:
//  1. Empty URL → allowed (provider may not need a custom base).
//  2. Claude CLI → api_base is an executable path/command, not a URL.
//  3. Scheme check (http/https only) → enforced for URL-based types, including
//     local URL types. Blocks file://, gopher://, dict://, etc.
//  4. Local URL types (ollama, acp) → host must be in allowedLocalHosts, or in
//     the operator-configured GOCLAW_OLLAMA_ALLOWED_HOSTS list (explicit
//     allowlist prevents reaching 169.254.169.254 or internal services via the
//     local-type bypass, while still allowing LAN-hosted Ollama servers).
//  5. Remote types → if GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS is set, allow and log.
//     Otherwise: resolve DNS hostname; reject if ANY resolved IP satisfies
//     security.IsBlocked (covers loopback, link-local, private, multicast,
//     unspecified — including 0.0.0.0 and :: that earlier hand-rolled checks missed).
//
// DNS resolution on step 5 closes the nip.io / sslip.io / attacker-domain bypass
// where a hostname passes a literal-string blocklist but resolves to a private IP.
func validateProviderURL(rawURL string, providerType string) error {
	if rawURL == "" {
		return nil
	}
	if providerType == store.ProviderClaudeCLI {
		return validateClaudeCLIExecutablePath(rawURL)
	}
	if providerType == store.ProviderACP {
		return validateACPExecutablePath(rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// Scheme check is unconditional for URL-based provider types, including local URL types.
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("provider URL must use http or https scheme, got %q", u.Scheme)
	}

	host := u.Hostname()

	// Local provider types: only allow an explicit localhost allowlist.
	// This prevents using the local-type escape hatch to reach internal services
	// or cloud metadata endpoints.
	if localURLProviderTypes[providerType] {
		for _, a := range allowedLocalHosts {
			if strings.EqualFold(host, a) {
				return nil
			}
		}
		for _, a := range ollamaAllowedHostsFn() {
			if strings.EqualFold(host, a) {
				return nil
			}
		}
		slog.Warn("security.provider_url.local_type_denied", "host", host, "provider_type", providerType)
		return fmt.Errorf("provider type %q only allows localhost URLs (localhost, 127.0.0.1, ::1, host.docker.internal, or GOCLAW_OLLAMA_ALLOWED_HOSTS), got host %q", providerType, host)
	}

	// Operator opt-in to allow private-network provider URLs (e.g. LAN-hosted vLLM).
	// Scheme check above still applies even with this gate open.
	if allowPrivateProviderURLsFn() {
		slog.Warn("security.provider_url.private_allowed", "host", host, "provider_type", providerType)
		return nil
	}

	// Check literal IP first (avoids unnecessary DNS lookup).
	if ip := net.ParseIP(host); ip != nil {
		if security.IsBlocked(ip) {
			slog.Warn("security.provider_url.blocked", "host", host, "provider_type", providerType)
			return fmt.Errorf("provider URL cannot point to %s", host)
		}
		return nil
	}

	// Block .internal / .local suffix before DNS (fail-fast for well-known patterns).
	if strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		slog.Warn("security.provider_url.blocked", "host", host, "provider_type", providerType)
		return fmt.Errorf("provider URL cannot point to internal hostname: %s", host)
	}

	// Resolve DNS and check every returned address.
	// Prevents bypass via wildcard services (nip.io, sslip.io) or attacker-controlled
	// domains that map to private IPs (DNS-rebinding at config time).
	addrs, err := dnsResolverFn(host)
	if err != nil {
		slog.Warn("security.provider_url.dns_resolve_failed", "host", host, "provider_type", providerType, "error", err)
		return fmt.Errorf("provider URL hostname %q could not be resolved: %w", host, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if security.IsBlocked(ip) {
			slog.Warn("security.provider_url.blocked_resolved", "host", host, "resolved_ip", ip.String(), "provider_type", providerType)
			return fmt.Errorf("provider URL %q resolves to private/reserved address %s", host, ip)
		}
	}
	return nil
}

func validateClaudeCLIExecutablePath(path string) error {
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("Claude CLI executable path cannot contain NUL byte")
	}
	if _, err := url.ParseRequestURI(path); err == nil && strings.Contains(path, "://") {
		return fmt.Errorf("Claude CLI api_base must be an executable path or %q, got URL %q", "claude", path)
	}
	if path == "claude" || filepath.IsAbs(path) {
		return nil
	}
	return fmt.Errorf("Claude CLI api_base must be %q or an absolute executable path, got %q", "claude", path)
}

// validateACPExecutablePath validates that api_base for ACP providers carries a
// command or absolute executable path, not a URL. This mirrors the runtime
// registration logic in cmd/gateway_providers.go.
func validateACPExecutablePath(path string) error {
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("ACP binary path cannot contain NUL byte")
	}
	if _, err := url.ParseRequestURI(path); err == nil && strings.Contains(path, "://") {
		return fmt.Errorf("ACP api_base must be an executable path or command, got URL %q", path)
	}
	// Keep parity with registerACPFromDB: built-in command names or absolute paths.
	if path == "claude" || path == "codex" || path == "gemini" || filepath.IsAbs(path) {
		return nil
	}
	return fmt.Errorf("ACP api_base must be %q, %q, %q, or an absolute executable path, got %q", "claude", "codex", "gemini", path)
}

// --- Provider CRUD ---

func (h *ProvidersHandler) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.store.ListProviders(r.Context())
	if err != nil {
		slog.Error("providers.list", "error", err)
		locale := extractLocale(r)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "providers")})
		return
	}

	for i := range providers {
		maskAPIKey(&providers[i])
	}

	publicProviders := make([]store.LLMProviderData, 0, len(providers))
	for i := range providers {
		publicProviders = append(publicProviders, canonicalizeProviderForResponse(&providers[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": publicProviders})
}

func (h *ProvidersHandler) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	var p store.LLMProviderData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if p.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name")})
		return
	}
	if !isValidSlug(p.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
		return
	}
	if !store.ValidProviderTypes[p.ProviderType] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "unsupported provider_type")})
		return
	}

	// exec_path is a migration-written column (the legacy api_base of a CLI
	// provider) and it is authoritative for the executable the gateway runs. It
	// is not a client input: accepting it here would let a caller choose that
	// binary without passing the CLI-executable check api_base already has to
	// pass. CLI providers are created through api_base; phase 6 can expose the
	// declaration deliberately, with a matching validator.
	if strings.TrimSpace(p.ExecPath) != "" {
		slog.Warn("security.provider_exec_path.rejected",
			"name", p.Name, "provider_type", p.ProviderType, "exec_path", p.ExecPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": i18n.T(locale, i18n.MsgInvalidRequest, "exec_path is not accepted; set api_base for CLI providers"),
		})
		return
	}

	// Only one Claude CLI provider is allowed per instance (1 machine = 1 auth session).
	// Mutex serializes check+create to prevent TOCTOU race.
	if p.ProviderType == store.ProviderClaudeCLI {
		h.cliMu.Lock()
		defer h.cliMu.Unlock()

		existing, _ := h.store.ListProviders(r.Context())
		for _, ep := range existing {
			if ep.ProviderType == store.ProviderClaudeCLI {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": i18n.T(locale, i18n.MsgAlreadyExists, "Claude CLI provider", "only one is allowed per instance"),
				})
				return
			}
		}
	}

	if err := validateChatGPTOAuthProviderCandidate(r.Context(), h.store, uuid.Nil, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateProviderEmbeddingSettings(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	if err := validateProviderURL(p.APIBase, p.ProviderType); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Normalize Ollama base URL to include /v1 so all code paths
	// (chat, model listing, embedding verify) use the same value from DB.
	normalizeOllamaAPIBase(&p)

	if err := h.store.CreateProvider(r.Context(), &p); err != nil {
		slog.Error("providers.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Register in-memory so verify/chat work without restart
	h.registerInMemory(&p)
	h.emitProviderCacheInvalidate(r.Context(), p.TenantID, p.Name)

	emitAudit(h.msgBus, r, "provider.created", "provider", p.ID.String())
	maskAPIKey(&p)
	publicProvider := canonicalizeProviderForResponse(&p)
	writeJSON(w, http.StatusCreated, publicProvider)
}

func (h *ProvidersHandler) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	maskAPIKey(p)
	publicProvider := canonicalizeProviderForResponse(p)
	writeJSON(w, http.StatusOK, publicProvider)
}

func (h *ProvidersHandler) handleReconnectProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	var req struct {
		Verify bool `json:"verify"`
	}
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
			return
		}
		if req.Verify {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "verify is not supported by reconnect; call provider verify separately")})
			return
		}
	}

	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	if h.providerReg != nil {
		h.providerReg.UnregisterForTenant(p.TenantID, p.Name)
	}

	status := "disabled"
	registryUpdated := false
	if p.Enabled {
		switch h.registerInMemory(p) {
		case providerRuntimeRegistered:
			status = "reconnected"
			registryUpdated = true
		default:
			status = "not_registered"
		}
	}
	cacheInvalidated := h.emitProviderCacheInvalidate(r.Context(), p.TenantID, p.Name)
	emitAudit(h.msgBus, r, "provider.reconnected", "provider", p.ID.String())

	maskAPIKey(p)
	publicProvider := canonicalizeProviderForResponse(p)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            status,
		"provider":          publicProvider,
		"registry_updated":  registryUpdated,
		"cache_invalidated": cacheInvalidated,
	})
}

func (h *ProvidersHandler) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Validate name if being updated
	if name, ok := updates["name"]; ok {
		if s, _ := name.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
			return
		}
	}

	// Strip masked API key — don't overwrite real value with "***"
	if apiKey, ok := updates["api_key"]; ok {
		if s, _ := apiKey.(string); s == "***" || s == "" {
			delete(updates, "api_key")
		}
	}

	// Allowlist: only permit known provider columns.
	updates = filterAllowedKeys(updates, providerAllowedFields)

	currentProvider, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	candidate := *currentProvider
	if name, ok := updates["name"].(string); ok && name != "" {
		candidate.Name = name
	}
	if apiKey, ok := updates["api_key"].(string); ok {
		candidate.APIKey = apiKey
	}
	if apiBase, ok := updates["api_base"].(string); ok {
		candidate.APIBase = apiBase
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		candidate.Enabled = enabled
	}
	if displayName, ok := updates["display_name"].(string); ok {
		candidate.DisplayName = displayName
	}
	if pt, ok := updates["provider_type"].(string); ok && pt != "" {
		candidate.ProviderType = pt
	}
	if settings, ok := updates["settings"]; ok {
		rawSettings, err := marshalJSONRaw(settings)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
			return
		}
		candidate.Settings = rawSettings
		updates["settings"] = rawSettings
	}

	// Re-validate URLs against the (possibly new) provider type.
	// When provider_type changes, existing api_base must also pass validation
	// for the new type — prevents SSRF via ACP→non-ACP type switch.
	typeChanged := candidate.ProviderType != currentProvider.ProviderType

	if apiBase, ok := updates["api_base"]; ok {
		if s, _ := apiBase.(string); s != "" {
			if err := validateProviderURL(s, candidate.ProviderType); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	} else if typeChanged && candidate.APIBase != "" {
		if err := validateProviderURL(candidate.APIBase, candidate.ProviderType); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if baseURL, ok := updates["base_url"]; ok {
		if s, _ := baseURL.(string); s != "" {
			if err := validateProviderURL(s, candidate.ProviderType); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	}

	// Normalize Ollama base URL to include /v1 so all code paths use the same value.
	normalizeOllamaAPIBase(&candidate)
	if candidate.APIBase != currentProvider.APIBase {
		updates["api_base"] = candidate.APIBase
	}

	if err := validateChatGPTOAuthProviderCandidate(r.Context(), h.store, id, &candidate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateProviderEmbeddingSettings(&candidate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	// Track old name before update for registry cleanup
	var oldName string
	if h.providerReg != nil {
		oldName = currentProvider.Name
	}

	if err := h.store.UpdateProvider(r.Context(), id, updates); err != nil {
		slog.Error("providers.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Sync in-memory registry with updated provider
	if h.providerReg != nil {
		if updated, err := h.store.GetProvider(r.Context(), id); err == nil {
			// Unregister old name if renamed to prevent ghost entries
			if oldName != "" && oldName != updated.Name {
				h.providerReg.UnregisterForTenant(updated.TenantID, oldName)
			}
			if !updated.Enabled {
				h.providerReg.UnregisterForTenant(updated.TenantID, updated.Name)
			} else {
				h.registerInMemory(updated)
			}
		}
	}

	// Notify subscribers (e.g. ACP re-registration) about the change
	if updated, err := h.store.GetProvider(r.Context(), id); err == nil {
		h.emitProviderCacheInvalidate(r.Context(), updated.TenantID, updated.Name)
		if oldName != "" && oldName != updated.Name {
			h.emitProviderCacheInvalidate(r.Context(), updated.TenantID, oldName)
		}
	}

	emitAudit(h.msgBus, r, "provider.updated", "provider", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ProvidersHandler) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	// Read provider before deleting so we can unregister it
	var providerName string
	var providerTenantID uuid.UUID
	if p, err := h.store.GetProvider(r.Context(), id); err == nil {
		providerName = p.Name
		providerTenantID = p.TenantID
	}

	if err := h.store.DeleteProvider(r.Context(), id); err != nil {
		slog.Error("providers.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.providerReg != nil && providerName != "" {
		h.providerReg.UnregisterForTenant(providerTenantID, providerName)
	}
	if providerName != "" {
		h.emitProviderCacheInvalidate(r.Context(), providerTenantID, providerName)
	}

	emitAudit(h.msgBus, r, "provider.deleted", "provider", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Provider health (provider rework, phase 5) ---

// Provider health resource:
//
//	GET  /v1/providers/{id}/health  → durable cooldown / error-class state (Viewer)
//	POST /v1/providers/{id}/health  → {"reset":true} clears the persisted state,
//	                                  {"probe":true[,"model":"m"]} actively probes
//	                                  the provider through the shared verify path
//	                                  and records the outcome (Admin)
//
// The active probe is only ever triggered by an explicit request — there is no
// background prober (phase 5 forbids one), so idle gateways never burn tokens.
func (h *ProvidersHandler) handleProviderHealthRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.readAuth(h.handleProviderHealth)(w, r)
	case http.MethodPost:
		h.auth(h.handleProviderHealthAction)(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": i18n.T(extractLocale(r), i18n.MsgInvalidRequest, r.Method+" is not supported on provider health"),
		})
	}
}

// providerForHealth parses the {id} path value and loads the provider row,
// writing the 400/404 response itself when that fails.
func (h *ProvidersHandler) providerForHealth(w http.ResponseWriter, r *http.Request) (*store.LLMProviderData, bool) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return nil, false
	}
	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return nil, false
	}
	return p, true
}

// handleProviderHealth reports the durable health of one provider.
func (h *ProvidersHandler) handleProviderHealth(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	p, ok := h.providerForHealth(w, r)
	if !ok {
		return
	}
	health, err := h.store.GetProviderHealth(r.Context(), p.ID)
	if err != nil {
		slog.Error("providers.health_read_failed", "provider", p.Name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": i18n.T(locale, i18n.MsgProviderHealthFailed, p.Name),
		})
		return
	}
	writeJSON(w, http.StatusOK, providerHealthPayload(p, health, nil))
}

// handleProviderHealthAction applies one operator action (reset and/or probe) and
// returns the resulting health, so a single round trip shows the new state.
func (h *ProvidersHandler) handleProviderHealthAction(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	var req struct {
		Reset bool   `json:"reset"`
		Probe bool   `json:"probe"`
		Model string `json:"model"`
	}
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
			return
		}
	}
	if !req.Reset && !req.Probe {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": i18n.T(locale, i18n.MsgInvalidRequest, "reset or probe is required"),
		})
		return
	}

	p, ok := h.providerForHealth(w, r)
	if !ok {
		return
	}

	// reset clears the persisted cooldown/failure state so the next call retries
	// the provider immediately: the manual escape hatch for a cooldown that
	// outlived the outage that caused it (`goclaw providers health --reset`).
	if req.Reset {
		if err := h.store.ResetProviderHealth(r.Context(), p.ID); err != nil {
			slog.Error("providers.health_reset_failed", "provider", p.Name, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": i18n.T(locale, i18n.MsgProviderHealthResetFailed, p.Name, err.Error()),
			})
			return
		}
		emitAudit(h.msgBus, r, "provider.health_reset", "provider", p.ID.String())
	}

	var probe *providerProbeResult
	if req.Probe {
		probe = h.runProviderProbe(r.Context(), p, req.Model)
	}

	health, err := h.store.GetProviderHealth(r.Context(), p.ID)
	if err != nil {
		slog.Error("providers.health_read_failed", "provider", p.Name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": i18n.T(locale, i18n.MsgProviderHealthFailed, p.Name),
		})
		return
	}
	writeJSON(w, http.StatusOK, providerHealthPayload(p, health, probe))
}

// providerProbeResult is the outcome of one explicitly requested active probe.
type providerProbeResult struct {
	Valid bool `json:"valid"`
	// Mode is "model" when the probe sent a chat request, "reachability" when no
	// model could be resolved and only registration/transport was checked.
	Mode       string `json:"mode"`
	Model      string `json:"model,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
}

// runProviderProbe probes the provider through the shared verify path
// (provider_verify.go) and records the outcome in provider_health: a failure
// starts a cooldown with the same duration rules as the runtime path
// (providers.CooldownDurationFor), a success clears the cooldown.
//
// The probe reuses provider verify by invoking its handler with a captured
// ResponseWriter, so probe semantics cannot drift from `providers verify`.
func (h *ProvidersHandler) runProviderProbe(ctx context.Context, p *store.LLMProviderData, requestedModel string) *providerProbeResult {
	model := h.probeModel(p, requestedModel)
	result := &providerProbeResult{Mode: "reachability", Model: model}
	if model != "" {
		result.Mode = "model"
	}

	body, _ := json.Marshal(map[string]string{"model": model})
	path := "/v1/providers/" + p.ID.String() + "/verify"
	verifyReq, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		result.Error = err.Error()
		result.ErrorClass = string(store.ErrorClassUnknown)
		return result
	}
	verifyReq.SetPathValue("id", p.ID.String())
	recorder := &healthProbeRecorder{header: http.Header{}}
	h.handleVerifyProvider(recorder, verifyReq)

	var verify struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.body.Bytes(), &verify); err != nil {
		result.Error = "provider verify returned an unreadable response"
		result.ErrorClass = string(store.ErrorClassUnknown)
		return result
	}
	result.Valid = verify.Valid
	result.Error = verify.Error

	// Probe timestamps are recorded either way so the health surface can tell
	// "no traffic" apart from "not re-checked".
	if err := h.store.MarkProviderProbe(ctx, p.ID); err != nil {
		slog.Warn("providers.health_probe_mark_failed", "provider", p.Name, "error", err)
	}
	if verify.Valid {
		result.ErrorClass = ""
		if err := h.store.RecordProviderSuccess(ctx, p.ID); err != nil {
			slog.Warn("providers.health_probe_success_failed", "provider", p.Name, "error", err)
		}
		return result
	}

	// Classify the friendly message verify produced. It has already been stripped
	// of the "HTTP <status>: <provider>:" prefix, so status-based classes
	// (rate_limit etc.) usually collapse to "unknown" here; the runtime path
	// classifies the raw error and stays the authoritative source.
	classification := providers.ClassifyHTTPError(providers.NewDefaultClassifier(), errors.New(result.Error))
	reason := classification.Reason
	if classification.Kind != "reason" || reason == "" {
		reason = providers.FailoverUnknown
	}
	result.ErrorClass = string(reason)
	if err := h.store.RecordProviderFailure(ctx, p.ID, result.ErrorClass, time.Now().UTC().Add(providers.CooldownDurationFor(reason))); err != nil {
		slog.Warn("providers.health_probe_failure_failed", "provider", p.Name, "error", err)
	}
	return result
}

// probeModel resolves the model an active probe should use: the requested model,
// else the registered provider's own default. An empty result means the probe can
// only check registration/transport (verify's ping mode).
func (h *ProvidersHandler) probeModel(p *store.LLMProviderData, requested string) string {
	if requested != "" {
		return requested
	}
	if h.providerReg == nil {
		return ""
	}
	provider, err := h.providerReg.GetForTenant(p.TenantID, p.Name)
	if err != nil || provider == nil {
		return ""
	}
	return provider.DefaultModel()
}

// healthProbeRecorder captures a handler's JSON response without a real client.
// Only Write/WriteHeader/Header are used by the verify handler.
type healthProbeRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *healthProbeRecorder) Header() http.Header { return r.header }

func (r *healthProbeRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *healthProbeRecorder) WriteHeader(status int) { r.status = status }

// providerHealthPayload is the flat health shape the CLI/UI consume:
// jq-friendly (`.[0] | {provider, cooldown_until, consecutive_failures}`).
func providerHealthPayload(p *store.LLMProviderData, health *store.ProviderHealth, probe *providerProbeResult) map[string]any {
	payload := map[string]any{
		"provider_id":          p.ID.String(),
		"provider":             p.Name,
		"enabled":              p.Enabled,
		"consecutive_failures": health.ConsecutiveFailures,
		"cooling_down":         health.CoolingDown(time.Now().UTC()),
		"cooldown_until":       nil,
		"last_error_class":     health.LastErrorClass,
		"error_counts":         health.ErrorCounts,
		"last_probe_at":        nil,
		"updated_at":           nil,
		// The ceiling the runtime enforces, so the UI can explain why a cooldown
		// never exceeds it (phase 5: unbounded cooldown is a risk).
		"max_cooldown_seconds": int(providers.MaxCooldown.Seconds()),
	}
	if health.CooldownUntil != nil {
		payload["cooldown_until"] = health.CooldownUntil.UTC()
	}
	if health.LastProbeAt != nil {
		payload["last_probe_at"] = health.LastProbeAt.UTC()
	}
	if !health.UpdatedAt.IsZero() {
		payload["updated_at"] = health.UpdatedAt.UTC()
	}
	if probe != nil {
		payload["probe"] = probe
	}
	return payload
}
