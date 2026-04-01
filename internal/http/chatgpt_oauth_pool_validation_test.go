package http

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestValidateChatGPTOAuthProviderCandidateRejectsMemberReuseAcrossPools(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	for _, provider := range []*store.LLMProviderData{
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "openai-codex",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      true,
			Settings: json.RawMessage(`{
				"codex_pool": {
					"strategy": "round_robin",
					"extra_provider_names": ["codex-work"]
				}
			}`),
		},
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-work",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      true,
		},
	} {
		if err := providerStore.CreateProvider(ctx, provider); err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
	}

	candidate := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "codex-team",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "priority_order",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}

	if err := validateChatGPTOAuthProviderCandidate(ctx, providerStore, uuid.Nil, candidate); err == nil {
		t.Fatal("validateChatGPTOAuthProviderCandidate() = nil, want membership conflict")
	}
}

func TestValidateChatGPTOAuthProviderCandidateRejectsPoolOnMember(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	owner := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "round_robin",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}
	member := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "codex-work",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
	}

	if err := providerStore.CreateProvider(ctx, owner); err != nil {
		t.Fatalf("CreateProvider(owner) error = %v", err)
	}
	if err := providerStore.CreateProvider(ctx, member); err != nil {
		t.Fatalf("CreateProvider(member) error = %v", err)
	}

	memberWithPool := *member
	memberWithPool.Settings = json.RawMessage(`{
		"codex_pool": {
			"strategy": "priority_order",
			"extra_provider_names": ["codex-backup"]
		}
	}`)

	if err := validateChatGPTOAuthProviderCandidate(ctx, providerStore, member.ID, &memberWithPool); err == nil {
		t.Fatal("validateChatGPTOAuthProviderCandidate() = nil, want member-owner conflict")
	}
}

func TestValidateChatGPTOAuthAgentRoutingRejectsCustomMembersWithoutProviderPool(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	if err := providerStore.CreateProvider(ctx, &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
	}); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	routing := &store.ChatGPTOAuthRoutingConfig{
		OverrideMode:       store.ChatGPTOAuthOverrideCustom,
		Strategy:           store.ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work"},
	}

	if err := validateChatGPTOAuthAgentRouting(ctx, providerStore, "openai-codex", routing); err == nil {
		t.Fatal("validateChatGPTOAuthAgentRouting() = nil, want provider-owned pool error")
	}
}

func TestValidateChatGPTOAuthAgentRoutingAllowsStrategyOnlyOverride(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	if err := providerStore.CreateProvider(ctx, &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "round_robin",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	routing := &store.ChatGPTOAuthRoutingConfig{
		OverrideMode: store.ChatGPTOAuthOverrideCustom,
		Strategy:     store.ChatGPTOAuthStrategyPriority,
	}

	if err := validateChatGPTOAuthAgentRouting(ctx, providerStore, "openai-codex", routing); err != nil {
		t.Fatalf("validateChatGPTOAuthAgentRouting() error = %v, want nil", err)
	}
}

// TestValidatePoolGraphIgnoresDisabledProviders verifies that disabled providers'
// stale pool configs do not block validation for active providers.
func TestValidatePoolGraphIgnoresDisabledProviders(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	// Disabled provider that previously owned "codex-work" in its pool.
	for _, p := range []*store.LLMProviderData{
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-old",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      false, // disabled
			Settings: json.RawMessage(`{
				"codex_pool": {
					"strategy": "round_robin",
					"extra_provider_names": ["codex-work"]
				}
			}`),
		},
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-work",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      true,
		},
	} {
		if err := providerStore.CreateProvider(ctx, p); err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
	}

	// New candidate claims "codex-work" — should pass because the old owner is disabled.
	candidate := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "codex-new",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "primary_first",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}

	if err := validateChatGPTOAuthProviderCandidate(ctx, providerStore, uuid.Nil, candidate); err != nil {
		t.Fatalf("validateChatGPTOAuthProviderCandidate() error = %v, want nil (disabled owner should be ignored)", err)
	}
}

// TestValidatePoolGraphRejectsConflictWithEnabledProviders ensures that enabled
// providers still enforce the exclusive-membership rule.
func TestValidatePoolGraphRejectsConflictWithEnabledProviders(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	for _, p := range []*store.LLMProviderData{
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-A",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      true,
			Settings: json.RawMessage(`{
				"codex_pool": {
					"strategy": "round_robin",
					"extra_provider_names": ["codex-work"]
				}
			}`),
		},
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-work",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      true,
		},
	} {
		if err := providerStore.CreateProvider(ctx, p); err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
	}

	candidate := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "codex-B",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "primary_first",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}

	if err := validateChatGPTOAuthProviderCandidate(ctx, providerStore, uuid.Nil, candidate); err == nil {
		t.Fatal("validateChatGPTOAuthProviderCandidate() = nil, want membership conflict with enabled provider")
	}
}

// TestValidatePoolGraphAllowsReassignAfterDisable verifies that a member can be
// reassigned to a new pool after the original pool owner is disabled.
func TestValidatePoolGraphAllowsReassignAfterDisable(t *testing.T) {
	providerStore := newMockProviderStore()
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	for _, p := range []*store.LLMProviderData{
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-A",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      false, // previously active, now disabled
			Settings: json.RawMessage(`{
				"codex_pool": {
					"strategy": "round_robin",
					"extra_provider_names": ["codex-work"]
				}
			}`),
		},
		{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "codex-work",
			ProviderType: store.ProviderChatGPTOAuth,
			Enabled:      true,
		},
	} {
		if err := providerStore.CreateProvider(ctx, p); err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
	}

	candidate := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "codex-B",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "priority_order",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}

	if err := validateChatGPTOAuthProviderCandidate(ctx, providerStore, uuid.Nil, candidate); err != nil {
		t.Fatalf("validateChatGPTOAuthProviderCandidate() error = %v, want nil (disabled owner should allow reassignment)", err)
	}
}
