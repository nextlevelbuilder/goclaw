package cmd

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type stubReloadSystemConfigs struct {
	values       map[string]string
	lastTenantID uuid.UUID
}

func (s *stubReloadSystemConfigs) Get(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubReloadSystemConfigs) Set(_ context.Context, _ string, _ string) error { return nil }
func (s *stubReloadSystemConfigs) Delete(_ context.Context, _ string) error        { return nil }
func (s *stubReloadSystemConfigs) List(ctx context.Context) (map[string]string, error) {
	s.lastTenantID = store.TenantIDFromContext(ctx)
	return s.values, nil
}

type stubReloadConfigSecrets struct {
	values       map[string]string
	lastTenantID uuid.UUID
}

func (s *stubReloadConfigSecrets) Get(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubReloadConfigSecrets) Set(_ context.Context, _, _ string) error        { return nil }
func (s *stubReloadConfigSecrets) Delete(_ context.Context, _ string) error        { return nil }
func (s *stubReloadConfigSecrets) GetAll(ctx context.Context) (map[string]string, error) {
	s.lastTenantID = store.TenantIDFromContext(ctx)
	return s.values, nil
}

func TestSyncWebSearchToolRegistration_KeepsDDGFallbackEnabled(t *testing.T) {
	reg := tools.NewRegistry()
	cfg := config.Default()

	current := syncWebSearchToolRegistration(reg, nil, cfg)
	if current == nil {
		t.Fatal("expected web_search tool")
	}
	if _, ok := reg.Get("web_search"); !ok {
		t.Fatal("web_search tool should be registered")
	}

	cfg.Tools.Web.DuckDuckGo.Enabled = false
	current = syncWebSearchToolRegistration(reg, current, cfg)
	if current == nil {
		t.Fatal("expected DDG fallback to keep web_search enabled")
	}
	if _, ok := reg.Get("web_search"); !ok {
		t.Fatal("web_search tool should remain registered")
	}
}

func TestSyncWebSearchToolRegistration_CreatesToolForConfiguredAPIProvider(t *testing.T) {
	reg := tools.NewRegistry()
	cfg := config.Default()
	cfg.Tools.Web.DuckDuckGo.Enabled = false
	cfg.Tools.Web.Exa.Enabled = true
	cfg.Tools.Web.Exa.APIKey = "exa-secret"

	current := syncWebSearchToolRegistration(reg, nil, cfg)
	if current == nil {
		t.Fatal("expected web_search tool")
	}
	if _, ok := reg.Get("web_search"); !ok {
		t.Fatal("web_search tool should be registered")
	}
}

func TestResolveMasterConfig_AppliesMasterScopedWebSearchOverrides(t *testing.T) {
	cfg := config.Default()
	systemConfigs := &stubReloadSystemConfigs{
		values: map[string]string{
			"tools.web.provider_order":     "brave,exa,tavily,duckduckgo",
			"tools.web.brave.enabled":      "true",
			"tools.web.brave.max_results":  "9",
			"tools.web.duckduckgo.enabled": "true",
		},
	}
	secrets := &stubReloadConfigSecrets{
		values: map[string]string{
			"tools.web.brave.api_key": "master-brave-secret",
		},
	}

	resolved := resolveMasterConfig(cfg, &store.Stores{
		SystemConfigs: systemConfigs,
		ConfigSecrets: secrets,
	})

	if resolved.Tools.Web.Brave.APIKey != "master-brave-secret" {
		t.Fatalf("brave api key = %q, want master-brave-secret", resolved.Tools.Web.Brave.APIKey)
	}
	if !resolved.Tools.Web.Brave.Enabled || resolved.Tools.Web.Brave.MaxResults != 9 {
		t.Fatalf("brave config = %+v, want enabled with max_results 9", resolved.Tools.Web.Brave)
	}
	if !resolved.Tools.Web.DuckDuckGo.Enabled {
		t.Fatal("duckduckgo fallback should stay enabled in resolved master config")
	}
	if got := resolved.Tools.Web.ProviderOrder; len(got) != 4 || got[0] != "brave" || got[3] != "duckduckgo" {
		t.Fatalf("provider_order = %v, want brave-first with duckduckgo last", got)
	}
	if systemConfigs.lastTenantID != store.MasterTenantID {
		t.Fatalf("system configs tenant = %s, want master", systemConfigs.lastTenantID)
	}
	if secrets.lastTenantID != store.MasterTenantID {
		t.Fatalf("config secrets tenant = %s, want master", secrets.lastTenantID)
	}
}

func TestIsMasterConfigEvent(t *testing.T) {
	if !isMasterConfigEvent(bus.Event{}) {
		t.Fatal("zero tenant event should be treated as master-scoped")
	}
	if !isMasterConfigEvent(bus.Event{TenantID: store.MasterTenantID}) {
		t.Fatal("master tenant event should be treated as master-scoped")
	}
	if isMasterConfigEvent(bus.Event{TenantID: uuid.New()}) {
		t.Fatal("non-master tenant event should not be treated as master-scoped")
	}
}
