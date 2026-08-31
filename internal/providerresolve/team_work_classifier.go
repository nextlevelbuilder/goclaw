package providerresolve

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TeamWorkClassifierSelection describes the provider/model pair used for the
// lightweight team-work routing arbiter.
type TeamWorkClassifierSelection struct {
	Provider     providers.Provider
	Model        string
	ProviderName string
	Source       string
	Warning      string
}

// ResolveTeamWorkClassifier resolves optional classifier-specific provider/model
// overrides without changing the default behavior: empty config uses the agent's
// runtime provider/model pair.
func ResolveTeamWorkClassifier(ctx context.Context, registry *providers.Registry, cfgProvider, cfgModel string, agentProvider providers.Provider, agentModel string) TeamWorkClassifierSelection {
	cfgProvider = strings.TrimSpace(cfgProvider)
	cfgModel = strings.TrimSpace(cfgModel)

	agentSelection := func(source, warning string) TeamWorkClassifierSelection {
		return TeamWorkClassifierSelection{
			Provider:     agentProvider,
			Model:        agentModel,
			ProviderName: providerName(agentProvider),
			Source:       source,
			Warning:      warning,
		}
	}

	if cfgProvider == "" && cfgModel == "" {
		return agentSelection("agent_default", "")
	}
	if cfgProvider == "" {
		return TeamWorkClassifierSelection{
			Provider:     agentProvider,
			Model:        cfgModel,
			ProviderName: providerName(agentProvider),
			Source:       "override_model_only",
		}
	}
	if registry == nil {
		return agentSelection("fallback_agent", fmt.Sprintf("classifier provider override %q ignored: provider registry unavailable", cfgProvider))
	}

	provider, err := registry.Get(ctx, cfgProvider)
	if err != nil || provider == nil {
		if err == nil {
			err = fmt.Errorf("provider not found")
		}
		return agentSelection("fallback_agent", fmt.Sprintf("classifier provider override %q invalid: %v", cfgProvider, err))
	}
	if cfgModel != "" {
		return TeamWorkClassifierSelection{
			Provider:     provider,
			Model:        cfgModel,
			ProviderName: providerName(provider),
			Source:       "override_provider_model",
		}
	}

	defaultModel := strings.TrimSpace(provider.DefaultModel())
	if defaultModel == "" {
		return agentSelection("fallback_agent", fmt.Sprintf("classifier provider override %q ignored: default model is empty", cfgProvider))
	}
	return TeamWorkClassifierSelection{
		Provider:     provider,
		Model:        defaultModel,
		ProviderName: providerName(provider),
		Source:       "override_provider_default_model",
	}
}

func providerName(provider providers.Provider) string {
	if provider == nil {
		return ""
	}
	return provider.Name()
}
