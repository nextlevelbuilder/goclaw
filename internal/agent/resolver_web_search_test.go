package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type stubSystemConfigStore struct {
	values map[string]string
}

func (s *stubSystemConfigStore) Get(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubSystemConfigStore) Set(_ context.Context, _ string, _ string) error { return nil }
func (s *stubSystemConfigStore) Delete(_ context.Context, _ string) error { return nil }
func (s *stubSystemConfigStore) List(_ context.Context) (map[string]string, error) {
	return s.values, nil
}

type stubResolverConfigSecretsStore struct {
	values map[string]string
}

func (s *stubResolverConfigSecretsStore) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}
func (s *stubResolverConfigSecretsStore) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}
func (s *stubResolverConfigSecretsStore) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}
func (s *stubResolverConfigSecretsStore) GetAll(_ context.Context) (map[string]string, error) {
	return s.values, nil
}

func TestResolveTenantConfig_AppliesTenantWebSearchOverrides(t *testing.T) {
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	base := config.Default()
	base.Tools.Web.ProviderOrder = []string{"exa"}

	resolved := resolveTenantConfig(ctx, ResolverDeps{
		BaseConfig:        base,
		SystemConfigStore: &stubSystemConfigStore{values: map[string]string{
			"tools.web.provider_order":       "brave,exa,tavily,duckduckgo",
			"tools.web.brave.enabled":        "true",
			"tools.web.brave.max_results":    "9",
			"tools.web.duckduckgo.max_results": "6",
		}},
		ConfigSecretsStore: &stubResolverConfigSecretsStore{values: map[string]string{
			"tools.web.brave.api_key": "tenant-brave-secret",
		}},
	}, tenantID)

	if resolved == nil {
		t.Fatal("expected resolved config")
	}
	if got := resolved.Tools.Web.ProviderOrder; len(got) != 4 || got[0] != "brave" || got[3] != "duckduckgo" {
		t.Fatalf("provider_order = %v, want brave-first with duckduckgo last", got)
	}
	if !resolved.Tools.Web.Brave.Enabled {
		t.Fatal("expected brave enabled from system configs")
	}
	if resolved.Tools.Web.Brave.MaxResults != 9 {
		t.Fatalf("brave max_results = %d, want 9", resolved.Tools.Web.Brave.MaxResults)
	}
	if resolved.Tools.Web.DuckDuckGo.MaxResults != 6 {
		t.Fatalf("duckduckgo max_results = %d, want 6", resolved.Tools.Web.DuckDuckGo.MaxResults)
	}
	if resolved.Tools.Web.Brave.APIKey != "tenant-brave-secret" {
		t.Fatalf("brave api key = %q, want tenant-brave-secret", resolved.Tools.Web.Brave.APIKey)
	}
}
