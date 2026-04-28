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
