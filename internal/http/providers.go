package http

import (
	"context"
	"encoding/json"
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
}

// NewProvidersHandler creates a handler for provider management endpoints.
func NewProvidersHandler(s store.ProviderStore, secretStore store.ConfigSecretsStore, providerReg *providers.Registry, gatewayAddr string) *ProvidersHandler {
	return &ProvidersHandler{store: s, secretStore: secretStore, providerReg: providerReg, gatewayAddr: gatewayAddr}
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
	// Provider CRUD
	mux.HandleFunc("GET /v1/providers", h.auth(h.handleListProviders))
	mux.HandleFunc("POST /v1/providers", h.auth(h.handleCreateProvider))
	mux.HandleFunc("GET /v1/providers/{id}", h.auth(h.handleGetProvider))
	mux.HandleFunc("PUT /v1/providers/{id}", h.auth(h.handleUpdateProvider))
	mux.HandleFunc("DELETE /v1/providers/{id}", h.auth(h.handleDeleteProvider))

	// Model listing (proxied to upstream provider API)
	mux.HandleFunc("GET /v1/providers/{id}/models", h.auth(h.handleListProviderModels))

	// Provider + model verification (pre-flight check)
	mux.HandleFunc("POST /v1/providers/{id}/reconnect", h.auth(h.handleReconnectProvider))
	mux.HandleFunc("POST /v1/providers/{id}/verify", h.auth(h.handleVerifyProvider))
	mux.HandleFunc("POST /v1/providers/{id}/verify-embedding", h.auth(h.handleVerifyEmbedding))

	// Provider-scoped Codex pool activity monitor
	mux.HandleFunc("GET /v1/providers/{id}/codex-pool-activity", h.auth(h.handleProviderCodexPoolActivity))

	// Embedding system status
	mux.HandleFunc("GET /v1/embedding/status", h.auth(h.handleEmbeddingStatus))

	// Claude CLI auth status (global — not per-provider)
	mux.HandleFunc("GET /v1/providers/claude-cli/auth-status", h.auth(h.handleClaudeCLIAuthStatus))
}

func (h *ProvidersHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
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
		prov := providers.NewOllamaProvider(p.Name, dockerHost, "llama3.3", numCtx, nil)
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
		prov := providers.NewOllamaProvider(p.Name, base, "llama3.3", numCtx, nil)
		h.providerReg.RegisterForTenant(p.TenantID, prov)
	default:
		base, model := openAIProviderDefaults(p.ProviderType, apiBase)
		prov := providers.NewOpenAIProvider(p.Name, p.APIKey, base, model)
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
	default:
		return apiBase, ""
	}
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
var localURLProviderTypes = map[string]bool{
	store.ProviderOllama: true,
	store.ProviderACP:    true,
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

// validateProviderURL rejects provider base URLs pointing to internal/private networks.
// Defense-in-depth: prevents SSRF when providers are later used for API calls.
//
// Logic:
//  1. Empty URL → allowed (provider may not need a custom base).
//  2. Claude CLI → api_base is an executable path/command, not a URL.
//  3. Scheme check (http/https only) → enforced for URL-based types, including
//     local URL types. Blocks file://, gopher://, dict://, etc.
//  4. Local URL types (ollama, acp) → host must be in allowedLocalHosts
//     (explicit allowlist prevents reaching 169.254.169.254 or internal services
//     via the local-type bypass).
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
		slog.Warn("security.provider_url.local_type_denied", "host", host, "provider_type", providerType)
		return fmt.Errorf("provider type %q only allows localhost URLs (localhost, 127.0.0.1, ::1, host.docker.internal), got host %q", providerType, host)
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
