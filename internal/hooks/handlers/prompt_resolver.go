package handlers

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// RegistryResolver adapts providers.Registry + store.SystemConfigStore into
// the ProviderResolver interface consumed by PromptHandler. It applies a
// simple fallback chain:
//
//  1. Explicit model alias → map to provider by convention (haiku/sonnet/opus → anthropic).
//  2. System config `hooks.prompt.provider` / `hooks.prompt.model`.
//  3. System config `background.provider` / `background.model`.
//  4. First registered provider for the tenant.
//
// Keeping this adapter in `handlers` rather than the higher-level
// `providerresolve` package avoids a new import cycle (providerresolve →
// store → hooks would introduce a diamond).
type RegistryResolver struct {
	Registry   *providers.Registry
	SysConfig  store.SystemConfigStore
	DefaultProviderForAlias func(alias string) string
}

// NewRegistryResolver returns a RegistryResolver with sensible defaults.
// registry MUST be non-nil. sysConfig may be nil (fallback to step 4 only).
func NewRegistryResolver(registry *providers.Registry, sysConfig store.SystemConfigStore) *RegistryResolver {
	return &RegistryResolver{
		Registry:               registry,
		SysConfig:              sysConfig,
		DefaultProviderForAlias: defaultProviderForAlias,
	}
}

// ResolveForHook implements ProviderResolver.
func (r *RegistryResolver) ResolveForHook(ctx context.Context, tenantID uuid.UUID, preferredModel string) (providers.Provider, string, error) {
	if r == nil || r.Registry == nil {
		return nil, "", errors.New("hook resolver: nil registry")
	}

	// Step 1: explicit alias → provider name.
	if preferredModel != "" {
		if name := r.providerForAlias(preferredModel); name != "" {
			if p, err := r.Registry.GetForTenant(tenantID, name); err == nil && p != nil {
				return p, preferredModel, nil
			}
		}
	}

	// Step 2: system config hooks.prompt.*
	configs := r.loadConfigs(ctx, tenantID)
	if p, m, ok := r.tryConfig(tenantID, configs["hooks.prompt.provider"], configs["hooks.prompt.model"], preferredModel); ok {
		return p, m, nil
	}

	// Step 3: fall back to background.*
	if p, m, ok := r.tryConfig(tenantID, configs["background.provider"], configs["background.model"], preferredModel); ok {
		return p, m, nil
	}

	// Step 4: first registered provider for the tenant.
	names := r.Registry.ListForTenant(tenantID)
	if len(names) == 0 {
		return nil, "", errors.New("hook resolver: no providers registered for tenant")
	}
	p, err := r.Registry.GetForTenant(tenantID, names[0])
	if err != nil || p == nil {
		return nil, "", err
	}
	model := preferredModel
	if model == "" {
		model = p.DefaultModel()
	}
	return p, model, nil
}

func (r *RegistryResolver) providerForAlias(alias string) string {
	if r.DefaultProviderForAlias == nil {
		return defaultProviderForAlias(alias)
	}
	return r.DefaultProviderForAlias(alias)
}

func (r *RegistryResolver) loadConfigs(ctx context.Context, tenantID uuid.UUID) map[string]string {
	if r.SysConfig == nil {
		return nil
	}
	tctx := store.WithTenantID(ctx, tenantID)
	configs, _ := r.SysConfig.List(tctx)
	return configs
}

func (r *RegistryResolver) tryConfig(tenantID uuid.UUID, name, cfgModel, preferred string) (providers.Provider, string, bool) {
	if name == "" {
		return nil, "", false
	}
	p, err := r.Registry.GetForTenant(tenantID, name)
	if err != nil || p == nil {
		return nil, "", false
	}
	model := preferred
	if model == "" {
		model = cfgModel
	}
	if model == "" {
		model = p.DefaultModel()
	}
	return p, model, true
}

// defaultProviderForAlias maps short model aliases to provider names.
// Unknown aliases return "" → resolver falls through to system-config step.
func defaultProviderForAlias(alias string) string {
	a := strings.ToLower(strings.TrimSpace(alias))
	switch {
	case strings.HasPrefix(a, "claude"), a == "haiku", a == "sonnet", a == "opus":
		return "anthropic"
	case strings.HasPrefix(a, "gpt"), strings.HasPrefix(a, "o1"), strings.HasPrefix(a, "o3"):
		return "openai"
	case strings.HasPrefix(a, "gemini"):
		return "google"
	case strings.HasPrefix(a, "qwen"):
		return "dashscope"
	}
	return ""
}
