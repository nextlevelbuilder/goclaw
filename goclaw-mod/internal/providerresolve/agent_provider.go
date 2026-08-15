package providerresolve

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ResolveConfiguredProvider resolves the provider an agent should actually use.
// It applies ChatGPT OAuth routing from the promoted agent routing field when present.
func ResolveConfiguredProvider(registry *providers.Registry, agent *store.AgentData) (providers.Provider, error) {
	if registry == nil || agent == nil {
		return nil, fmt.Errorf("provider registry unavailable")
	}

	baseProvider, baseErr := registry.GetForTenant(agent.TenantID, agent.Provider)
	if baseErr == nil {
		if _, ok := baseProvider.(*providers.CodexProvider); !ok {
			return baseProvider, nil
		}
	}

	var providerDefaults *store.ChatGPTOAuthRoutingConfig
	if codex, ok := baseProvider.(*providers.CodexProvider); ok {
		if defaults := codex.RoutingDefaults(); defaults != nil {
			providerDefaults = &store.ChatGPTOAuthRoutingConfig{
				Strategy:           defaults.Strategy,
				ExtraProviderNames: defaults.ExtraProviderNames,
			}
		}
	}
	if routing := store.ResolveEffectiveChatGPTOAuthRouting(providerDefaults, agent.ParseChatGPTOAuthRouting()); routing != nil {
		router := providers.NewChatGPTOAuthRouter(
			agent.TenantID,
			registry,
			agent.Provider,
			routing.Strategy,
			routing.ExtraProviderNames,
		)
		if router != nil && router.HasRegisteredProviders() {
			return router, nil
		}
	}

	if baseErr == nil {
		return baseProvider, nil
	}
	return nil, baseErr
}

// ResolveAgentProvider resolves the agent runtime provider, including generic
// per-agent model fallback when configured.
func ResolveAgentProvider(registry *providers.Registry, agent *store.AgentData) (providers.Provider, error) {
	baseProvider, err := ResolveConfiguredProvider(registry, agent)
	if err != nil {
		return nil, err
	}
	if registry == nil || agent == nil {
		return baseProvider, nil
	}
	fallbackCfg := agent.ParseModelFallback()
	if fallbackCfg == nil {
		return baseProvider, nil
	}
	candidates := make([]providers.FallbackCandidate, 0, len(fallbackCfg.Candidates))
	for _, candidate := range fallbackCfg.Candidates {
		provider, err := registry.GetForTenant(agent.TenantID, candidate.Provider)
		if err != nil || provider == nil {
			continue
		}
		candidates = append(candidates, providers.FallbackCandidate{
			ProviderName: candidate.Provider,
			Model:        candidate.Model,
			Provider:     provider,
		})
	}
	if len(candidates) == 0 {
		return baseProvider, nil
	}
	cooldownEnabled := true
	if fallbackCfg.CooldownEnabled != nil {
		cooldownEnabled = *fallbackCfg.CooldownEnabled
	}
	return providers.NewModelFallbackProvider(providers.FallbackCandidate{
		ProviderName: agent.Provider,
		Model:        agent.Model,
		Provider:     baseProvider,
	}, candidates, fallbackCfg.MaxAttempts, cooldownEnabled), nil
}
