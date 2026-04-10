package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/titanous/json5"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ConfigMethods handles config.get, config.apply, config.patch, config.schema.
// Matching TS src/gateway/server-methods/config.ts.
type ConfigMethods struct {
	cfg               *config.Config
	cfgPath           string
	secretsStore      store.ConfigSecretsStore
	systemConfigStore store.SystemConfigStore
	syncFn            func(ctx context.Context, cfg *config.Config) // nil-safe; syncs non-secret settings to system_configs
	eventBus          bus.EventPublisher                            // nil-safe; broadcasts config change events
}

func NewConfigMethods(cfg *config.Config, cfgPath string, secretsStore store.ConfigSecretsStore, systemConfigStore store.SystemConfigStore, eventBus bus.EventPublisher) *ConfigMethods {
	return &ConfigMethods{
		cfg:               cfg,
		cfgPath:           cfgPath,
		secretsStore:      secretsStore,
		systemConfigStore: systemConfigStore,
		eventBus:          eventBus,
	}
}

// SetSystemConfigSync sets a callback to sync config to system_configs after save.
// The callback receives the final resolved config (with secrets + env applied).
func (m *ConfigMethods) SetSystemConfigSync(fn func(ctx context.Context, cfg *config.Config)) {
	m.syncFn = fn
}

func (m *ConfigMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodConfigGet, m.requireOwner(m.handleGet))
	router.Register(protocol.MethodConfigApply, m.requireOwner(m.handleApply))
	router.Register(protocol.MethodConfigPatch, m.requireOwner(m.handlePatch))
	router.Register(protocol.MethodConfigSchema, m.requireOwner(m.handleSchema))
}

// requireOwner wraps a handler to only allow owner-role users.
func (m *ConfigMethods) requireOwner(next gateway.MethodHandler) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		if !client.IsOwner() {
			locale := store.LocaleFromContext(ctx)
			client.SendResponse(protocol.NewErrorResponse(
				req.ID, protocol.ErrUnauthorized,
				i18n.T(locale, i18n.MsgPermissionDenied, req.Method),
			))
			return
		}
		next(ctx, client, req)
	}
}

func (m *ConfigMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	resolved := m.resolveConfigForContext(ctx)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"config": resolved.MaskedCopy(),
		"hash":   resolved.Hash(),
		"path":   m.cfgPath,
	}))
}

// handleApply replaces the entire config with the provided JSON5 raw content.
// Matching TS config.apply (src/gateway/server-methods/config.ts:435-486).
func (m *ConfigMethods) handleApply(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Raw      string `json:"raw"`
		BaseHash string `json:"baseHash"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Raw == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRawConfigRequired)))
		return
	}

	currentCfg := m.resolveConfigForContext(ctx)

	// Optimistic concurrency: validate hash if provided
	if params.BaseHash != "" && params.BaseHash != currentCfg.Hash() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgConfigHashMismatch)))
		return
	}

	// Parse the new config
	newCfg := config.Default()
	if err := json5.Unmarshal([]byte(params.Raw), newCfg); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}
	normalizeWebSearchConfig(newCfg)
	if err := validateWebSearchConfig(newCfg); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}

	// Extract secrets → save to config_secrets table for this scope.
	if err := m.saveSecretsToStore(ctx, newCfg); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "config", err.Error())))
		return
	}

	if err := m.persistConfigForContext(ctx, newCfg); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "config", err.Error())))
		return
	}
	resolved := m.resolveConfigForContext(ctx)
	m.broadcastChanged(ctx, resolved)
	emitAudit(m.eventBus, client, "config.applied", "config", "gateway")

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"path":    m.cfgPath,
		"config":  resolved.MaskedCopy(),
		"hash":    resolved.Hash(),
		"restart": false,
	}))
}

// handlePatch merges a partial config update into the current config.
// Matching TS config.patch (src/gateway/server-methods/config.ts:321-434).
func (m *ConfigMethods) handlePatch(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Raw      string `json:"raw"`
		BaseHash string `json:"baseHash"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Raw == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRawPatchRequired)))
		return
	}

	currentCfg := m.resolveConfigForContext(ctx)

	// Optimistic concurrency
	if params.BaseHash != "" && params.BaseHash != currentCfg.Hash() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgConfigHashMismatch)))
		return
	}

	// Merge strategy: serialize current -> deserialize patch on top -> save
	currentJSON, err := json.Marshal(currentCfg)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "failed to serialize current config")))
		return
	}

	// Start from current config as base
	merged := config.Default()
	if err := json.Unmarshal(currentJSON, merged); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "failed to clone config")))
		return
	}

	// Apply patch on top
	if err := json5.Unmarshal([]byte(params.Raw), merged); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}
	normalizeWebSearchConfig(merged)
	if err := validateWebSearchConfig(merged); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}

	// Extract secrets → save to config_secrets table for this scope.
	if err := m.saveSecretsToStore(ctx, merged); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "config", err.Error())))
		return
	}

	if err := m.persistConfigForContext(ctx, merged); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "config", err.Error())))
		return
	}
	resolved := m.resolveConfigForContext(ctx)
	m.broadcastChanged(ctx, resolved)
	emitAudit(m.eventBus, client, "config.patched", "config", "gateway")

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"path":    m.cfgPath,
		"config":  resolved.MaskedCopy(),
		"hash":    resolved.Hash(),
		"restart": false,
	}))
}

func normalizeWebSearchConfig(cfg *config.Config) {
	cfg.Tools.Web.ProviderOrder = tools.NormalizeWebSearchProviderOrder(cfg.Tools.Web.ProviderOrder)
	cfg.Tools.Web.DuckDuckGo.Enabled = true
}

func validateWebSearchConfig(cfg *config.Config) error {
	if cfg.Tools.Web.Exa.Enabled && strings.TrimSpace(cfg.Tools.Web.Exa.APIKey) == "" {
		return fmt.Errorf("tools.web.exa.api_key is required when Exa is enabled")
	}
	if cfg.Tools.Web.Tavily.Enabled && strings.TrimSpace(cfg.Tools.Web.Tavily.APIKey) == "" {
		return fmt.Errorf("tools.web.tavily.api_key is required when Tavily is enabled")
	}
	if cfg.Tools.Web.Brave.Enabled && strings.TrimSpace(cfg.Tools.Web.Brave.APIKey) == "" {
		return fmt.Errorf("tools.web.brave.api_key is required when Brave is enabled")
	}
	return nil
}

// syncToSystemConfigs syncs the resolved config to system_configs table for the given tenant.
func (m *ConfigMethods) syncToSystemConfigs(ctx context.Context, cfg *config.Config) {
	if m.syncFn != nil {
		m.syncFn(ctx, cfg)
	}
}

// broadcastChanged notifies subscribers that config has been updated.
func (m *ConfigMethods) broadcastChanged(ctx context.Context, cfg *config.Config) {
	if m.eventBus != nil {
		m.eventBus.Broadcast(bus.Event{
			Name:     bus.TopicConfigChanged,
			Payload:  cfg,
			TenantID: store.TenantIDFromContext(ctx),
		})
	}
}

// handleSchema returns the config JSON schema for UI form generation.
// Matching TS config.schema (src/gateway/server-methods/config.ts:276-289).
func (m *ConfigMethods) handleSchema(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agents": map[string]any{
				"type":        "object",
				"description": "Agent configuration (defaults + per-agent overrides)",
			},
			"channels": map[string]any{
				"type":        "object",
				"description": "Channel configuration (telegram, discord, slack, etc.)",
			},
			"providers": map[string]any{
				"type":        "object",
				"description": "AI provider API keys and settings",
			},
			"gateway": map[string]any{
				"type":        "object",
				"description": "Gateway server settings (host, port, token)",
			},
			"tools": map[string]any{
				"type":        "object",
				"description": "Tool configuration (browser, exec, web search)",
			},
			"sessions": map[string]any{
				"type":        "object",
				"description": "Session storage configuration",
			},
		},
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"json": schema,
	}))
}

// saveSecretsToStore extracts non-LLM/non-channel secrets from the config
// and persists them to the config_secrets table.
func (m *ConfigMethods) saveSecretsToStore(ctx context.Context, cfg *config.Config) error {
	if m.secretsStore == nil {
		return nil
	}

	secrets := cfg.ExtractDBSecrets()
	currentValues := cfg.ManagedSecretValues()
	for _, key := range config.ManagedSecretKeys() {
		rawValue := currentValues[key]
		if config.IsMaskedSecret(rawValue) {
			continue
		}
		value, ok := secrets[key]
		if !ok {
			if err := m.secretsStore.Delete(ctx, key); err != nil {
				return fmt.Errorf("delete config secret %s: %w", key, err)
			}
			continue
		}
		if err := m.secretsStore.Set(ctx, key, value); err != nil {
			return fmt.Errorf("save config secret %s: %w", key, err)
		}
	}
	return nil
}

func (m *ConfigMethods) resolveConfigForContext(ctx context.Context) *config.Config {
	resolved := cloneConfigOrDefault(m.cfg)
	scopedCtx := tenantConfigContext(ctx)

	if m.systemConfigStore != nil {
		if values, err := m.systemConfigStore.List(scopedCtx); err == nil && len(values) > 0 {
			resolved.ApplySystemConfigs(values)
		}
	}
	if m.secretsStore != nil {
		if secrets, err := m.secretsStore.GetAll(scopedCtx); err == nil && len(secrets) > 0 {
			resolved.ApplyDBSecrets(secrets)
		}
	}

	resolved.ApplyEnvOverrides()
	normalizeWebSearchConfig(resolved)
	return resolved
}

func (m *ConfigMethods) persistConfigForContext(ctx context.Context, cfg *config.Config) error {
	if isMasterTenantContext(ctx) {
		toSave := cloneConfigOrDefault(cfg)
		toSave.StripSecrets()
		if err := config.Save(m.cfgPath, toSave); err != nil {
			return err
		}
		m.cfg.ReplaceFrom(toSave)
		if m.secretsStore != nil {
			if secrets, err := m.secretsStore.GetAll(tenantConfigContext(ctx)); err == nil {
				m.cfg.ApplyDBSecrets(secrets)
			}
		}
		m.cfg.ApplyEnvOverrides()
		normalizeWebSearchConfig(m.cfg)
		m.syncToSystemConfigs(ctx, m.cfg)
		return nil
	}

	scoped := cloneConfigOrDefault(cfg)
	scoped.StripSecrets()
	scoped.ApplyEnvOverrides()
	normalizeWebSearchConfig(scoped)
	m.syncToSystemConfigs(ctx, scoped)
	return nil
}

func cloneConfigOrDefault(base *config.Config) *config.Config {
	if base == nil {
		return config.Default()
	}
	data, err := json.Marshal(base)
	if err != nil {
		return config.Default()
	}
	cloned := config.Default()
	if err := json.Unmarshal(data, cloned); err != nil {
		return config.Default()
	}
	return cloned
}

func tenantConfigContext(ctx context.Context) context.Context {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	return store.WithTenantID(context.Background(), tenantID)
}

func isMasterTenantContext(ctx context.Context) bool {
	tenantID := store.TenantIDFromContext(ctx)
	return tenantID == uuid.Nil || tenantID == store.MasterTenantID
}
