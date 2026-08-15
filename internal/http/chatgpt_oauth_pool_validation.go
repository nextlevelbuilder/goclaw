package http

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func marshalJSONRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func normalizedProviderNamesForValidation(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func validateChatGPTOAuthPoolGraph(providers []store.LLMProviderData) error {
	providersByName := make(map[string]store.LLMProviderData, len(providers))
	for _, provider := range providers {
		providersByName[provider.Name] = provider
	}

	ownerByMember := map[string]string{}
	hasPoolConfig := map[string]bool{}

	for _, provider := range providers {
		settings := store.ParseChatGPTOAuthProviderSettings(provider.Settings)
		if settings == nil {
			continue
		}
		if provider.ProviderType != store.ProviderChatGPTOAuth {
			return fmt.Errorf("provider %q must be chatgpt_oauth to manage an OpenAI Codex pool", provider.Name)
		}

		hasPoolConfig[provider.Name] = true
		for _, memberName := range normalizedProviderNamesForValidation(settings.CodexPool.ExtraProviderNames) {
			if memberName == provider.Name {
				return fmt.Errorf("provider %q cannot include itself in its OpenAI Codex pool", provider.Name)
			}

			member, ok := providersByName[memberName]
			if !ok {
				// Stale reference — member was deleted or disabled. Skip
				// instead of blocking the pool owner from saving.
				continue
			}
			if member.ProviderType != store.ProviderChatGPTOAuth {
				return fmt.Errorf("provider %q can only add chatgpt_oauth members; %q is %s", provider.Name, memberName, member.ProviderType)
			}

			if existingOwner, ok := ownerByMember[memberName]; ok && existingOwner != provider.Name {
				return fmt.Errorf("provider %q already belongs to pool %q", memberName, existingOwner)
			}
			ownerByMember[memberName] = provider.Name
		}
	}

	for ownerName := range hasPoolConfig {
		if existingOwner, ok := ownerByMember[ownerName]; ok {
			return fmt.Errorf("provider %q already belongs to pool %q and cannot manage its own pool", ownerName, existingOwner)
		}
	}

	return nil
}

func validateChatGPTOAuthProviderCandidate(
	ctx context.Context,
	providerStore store.ProviderStore,
	currentID uuid.UUID,
	candidate *store.LLMProviderData,
) error {
	if providerStore == nil || candidate == nil {
		return nil
	}

	existingProviders, err := providerStore.ListProviders(ctx)
	if err != nil {
		return err
	}

	// Only consider chatgpt_oauth providers for pool graph validation.
	finalProviders := make([]store.LLMProviderData, 0, len(existingProviders)+1)
	replaced := false
	for _, provider := range existingProviders {
		if provider.ProviderType != store.ProviderChatGPTOAuth {
			continue
		}
		// Skip disabled providers — their stale pool configs should not
		// block validation for active providers.
		if !provider.Enabled {
			continue
		}
		if currentID != uuid.Nil && provider.ID == currentID {
			finalProviders = append(finalProviders, *candidate)
			replaced = true
			continue
		}
		finalProviders = append(finalProviders, provider)
	}
	if currentID == uuid.Nil || !replaced {
		finalProviders = append(finalProviders, *candidate)
	}

	if err := validateChatGPTOAuthPoolGraph(finalProviders); err != nil {
		return err
	}

	// Strip stale member references from the candidate's settings so they
	// don't accumulate as garbage in the DB after deleted/disabled providers.
	stripStalePoolMembers(candidate, finalProviders)
	return nil
}

// stripStalePoolMembers removes pool member names from the candidate's
// settings that no longer exist in the active provider set. This prevents
// deleted/disabled provider names from accumulating as garbage in the DB.
func stripStalePoolMembers(candidate *store.LLMProviderData, activeProviders []store.LLMProviderData) {
	if candidate == nil || len(candidate.Settings) == 0 {
		return
	}

	// Parse full settings as generic map to preserve non-pool fields.
	var raw map[string]any
	if json.Unmarshal(candidate.Settings, &raw) != nil {
		return
	}
	cpRaw, ok := raw["codex_pool"]
	if !ok {
		return
	}
	cpMap, ok := cpRaw.(map[string]any)
	if !ok {
		return
	}
	namesRaw, ok := cpMap["extra_provider_names"]
	if !ok {
		return
	}
	namesSlice, ok := namesRaw.([]any)
	if !ok || len(namesSlice) == 0 {
		return
	}

	known := make(map[string]bool, len(activeProviders))
	for _, p := range activeProviders {
		known[p.Name] = true
	}

	filtered := make([]any, 0, len(namesSlice))
	for _, n := range namesSlice {
		if name, ok := n.(string); ok && known[name] {
			filtered = append(filtered, name)
		}
	}

	if len(filtered) == len(namesSlice) {
		return // nothing changed
	}

	cpMap["extra_provider_names"] = filtered
	if updated, err := json.Marshal(raw); err == nil {
		candidate.Settings = updated
	}
}

func validateChatGPTOAuthAgentRouting(
	ctx context.Context,
	providerStore store.ProviderStore,
	providerName string,
	routing *store.ChatGPTOAuthRoutingConfig,
) error {
	if providerStore == nil || providerName == "" || routing == nil {
		return nil
	}

	tenantID := store.TenantIDFromContext(ctx)
	baseProvider, err := lookupProviderByNameWithMasterFallback(ctx, providerStore, tenantID, providerName)
	if err != nil || baseProvider == nil || baseProvider.ProviderType != store.ProviderChatGPTOAuth {
		return nil
	}

	defaultSettings := store.ParseChatGPTOAuthProviderSettings(baseProvider.Settings)
	defaultMembers := []string{}
	if defaultSettings != nil {
		defaultMembers = normalizedProviderNamesForValidation(defaultSettings.CodexPool.ExtraProviderNames)
	}

	if len(defaultMembers) == 0 {
		if len(routing.ExtraProviderNames) > 0 {
			return fmt.Errorf("configure OpenAI Codex pool members on provider %q before enabling agent-level routing", providerName)
		}
		return nil
	}

	if routing.OverrideMode == store.ChatGPTOAuthOverrideInherit {
		return nil
	}

	if len(routing.ExtraProviderNames) == 0 {
		return nil
	}

	if !slices.Equal(
		normalizedProviderNamesForValidation(routing.ExtraProviderNames),
		defaultMembers,
	) {
		return fmt.Errorf("agent routing cannot change pool membership for provider %q; manage members on the provider instead", providerName)
	}

	return nil
}
