package methods

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type stubConfigSecretsStore struct {
	values  map[string]string
	setKeys []string
	delKeys []string
}

func (s *stubConfigSecretsStore) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func (s *stubConfigSecretsStore) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	s.setKeys = append(s.setKeys, key)
	return nil
}

func (s *stubConfigSecretsStore) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	s.delKeys = append(s.delKeys, key)
	return nil
}

func (s *stubConfigSecretsStore) GetAll(_ context.Context) (map[string]string, error) {
	return s.values, nil
}

type stubScopedConfigSecretsStore struct {
	values map[uuid.UUID]map[string]string
}

func (s *stubScopedConfigSecretsStore) scope(ctx context.Context) map[string]string {
	if s.values == nil {
		s.values = map[uuid.UUID]map[string]string{}
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	if s.values[tenantID] == nil {
		s.values[tenantID] = map[string]string{}
	}
	return s.values[tenantID]
}

func (s *stubScopedConfigSecretsStore) Get(ctx context.Context, key string) (string, error) {
	return s.scope(ctx)[key], nil
}

func (s *stubScopedConfigSecretsStore) Set(ctx context.Context, key, value string) error {
	s.scope(ctx)[key] = value
	return nil
}

func (s *stubScopedConfigSecretsStore) Delete(ctx context.Context, key string) error {
	delete(s.scope(ctx), key)
	return nil
}

func (s *stubScopedConfigSecretsStore) GetAll(ctx context.Context) (map[string]string, error) {
	scope := s.scope(ctx)
	cloned := make(map[string]string, len(scope))
	for key, value := range scope {
		cloned[key] = value
	}
	return cloned, nil
}

type stubSystemConfigStore struct {
	values map[uuid.UUID]map[string]string
}

func (s *stubSystemConfigStore) scope(ctx context.Context) map[string]string {
	if s.values == nil {
		s.values = map[uuid.UUID]map[string]string{}
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	if s.values[tenantID] == nil {
		s.values[tenantID] = map[string]string{}
	}
	return s.values[tenantID]
}

func (s *stubSystemConfigStore) Get(ctx context.Context, key string) (string, error) {
	return s.scope(ctx)[key], nil
}

func (s *stubSystemConfigStore) Set(ctx context.Context, key, value string) error {
	s.scope(ctx)[key] = value
	return nil
}

func (s *stubSystemConfigStore) Delete(ctx context.Context, key string) error {
	delete(s.scope(ctx), key)
	return nil
}

func (s *stubSystemConfigStore) List(ctx context.Context) (map[string]string, error) {
	scope := s.scope(ctx)
	cloned := make(map[string]string, len(scope))
	for key, value := range scope {
		cloned[key] = value
	}
	return cloned, nil
}

func TestSaveSecretsToStore_DeletesClearedWebSearchSecrets(t *testing.T) {
	store := &stubConfigSecretsStore{
		values: map[string]string{
			"tools.web.exa.api_key":    "old-exa",
			"tools.web.tavily.api_key": "old-tavily",
			"tools.web.brave.api_key":  "old-brave",
		},
	}
	methods := &ConfigMethods{secretsStore: store}
	cfg := config.Default()
	cfg.Tools.Web.Exa.APIKey = ""
	cfg.Tools.Web.Tavily.APIKey = "new-tavily"
	cfg.Tools.Web.Brave.APIKey = ""

	if err := methods.saveSecretsToStore(context.Background(), cfg); err != nil {
		t.Fatalf("save secrets: %v", err)
	}

	if _, ok := store.values["tools.web.exa.api_key"]; ok {
		t.Fatal("expected Exa key deletion")
	}
	if _, ok := store.values["tools.web.brave.api_key"]; ok {
		t.Fatal("expected Brave key deletion")
	}
	if got := store.values["tools.web.tavily.api_key"]; got != "new-tavily" {
		t.Fatalf("tavily key = %q, want new-tavily", got)
	}
}

func TestSaveSecretsToStore_PreservesMaskedSecrets(t *testing.T) {
	store := &stubConfigSecretsStore{
		values: map[string]string{
			"tools.web.exa.api_key":   "old-exa",
			"tools.web.brave.api_key": "old-brave",
		},
	}
	methods := &ConfigMethods{secretsStore: store}
	cfg := config.Default()
	cfg.Tools.Web.Exa.APIKey = "***"
	cfg.Tools.Web.Brave.APIKey = "***"

	if err := methods.saveSecretsToStore(context.Background(), cfg); err != nil {
		t.Fatalf("save secrets: %v", err)
	}

	if got := store.values["tools.web.exa.api_key"]; got != "old-exa" {
		t.Fatalf("exa key = %q, want old-exa", got)
	}
	if got := store.values["tools.web.brave.api_key"]; got != "old-brave" {
		t.Fatalf("brave key = %q, want old-brave", got)
	}
}

func TestHandlePatch_PersistsCanonicalWebSearchOrderAndPreservesMaskedSecrets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.Default()
	cfg.Tools.Web.DuckDuckGo.Enabled = true
	cfg.Tools.Web.Exa.APIKey = "old-exa"

	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	store := &stubConfigSecretsStore{
		values: map[string]string{
			"tools.web.exa.api_key": "old-exa",
		},
	}
	methods := &ConfigMethods{
		cfg:          cfg,
		cfgPath:      cfgPath,
		secretsStore: store,
	}

	params := map[string]any{
		"raw":      `{"tools":{"web":{"provider_order":["duckduckgo","exa","brave","exa"],"exa":{"enabled":true,"api_key":"***","max_results":7},"brave":{"enabled":true,"api_key":"new-brave","max_results":3}}}}`,
		"baseHash": methods.resolveConfigForContext(context.Background()).Hash(),
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	req := &protocol.RequestFrame{
		ID:     "req-1",
		Method: protocol.MethodConfigPatch,
		Params: rawParams,
	}

	methods.handlePatch(context.Background(), &gateway.Client{}, req)

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	wantOrder := []string{"exa", "brave", "tavily", "duckduckgo"}
	if got := loaded.Tools.Web.ProviderOrder; len(got) != len(wantOrder) {
		t.Fatalf("provider_order len = %d, want %d (%v)", len(got), len(wantOrder), got)
	} else {
		for i := range wantOrder {
			if got[i] != wantOrder[i] {
				t.Fatalf("provider_order[%d] = %q, want %q (full=%v)", i, got[i], wantOrder[i], got)
			}
		}
	}

	if got := store.values["tools.web.exa.api_key"]; got != "old-exa" {
		t.Fatalf("exa key = %q, want old-exa", got)
	}
	if got := store.values["tools.web.brave.api_key"]; got != "new-brave" {
		t.Fatalf("brave key = %q, want new-brave", got)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected saved config file")
	}
}

func TestHandlePatch_TenantScopeDoesNotMutateMasterConfigOrSecrets(t *testing.T) {
	tenantID := uuid.New()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.Default()
	cfg.Tools.Web.DuckDuckGo.Enabled = true
	cfg.Tools.Web.ProviderOrder = []string{"brave", "tavily", "exa", "duckduckgo"}

	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	secretsStore := &stubScopedConfigSecretsStore{
		values: map[uuid.UUID]map[string]string{
			store.MasterTenantID: {
				"tools.web.exa.api_key": "master-exa",
			},
			tenantID: {
				"tools.web.exa.api_key": "tenant-exa",
			},
		},
	}
	systemStore := &stubSystemConfigStore{}

	methods := &ConfigMethods{
		cfg:               cfg,
		cfgPath:           cfgPath,
		secretsStore:      secretsStore,
		systemConfigStore: systemStore,
	}
	methods.SetSystemConfigSync(func(ctx context.Context, resolved *config.Config) {
		_ = systemStore.Set(ctx, "tools.web.provider_order", strings.Join(tools.NormalizeWebSearchProviderOrder(resolved.Tools.Web.ProviderOrder), ","))
		_ = systemStore.Set(ctx, "tools.web.exa.enabled", "true")
		_ = systemStore.Set(ctx, "tools.web.exa.max_results", "7")
		_ = systemStore.Set(ctx, "tools.web.brave.enabled", "true")
		_ = systemStore.Set(ctx, "tools.web.brave.max_results", "3")
		_ = systemStore.Set(ctx, "tools.web.duckduckgo.enabled", "true")
		_ = systemStore.Set(ctx, "tools.web.duckduckgo.max_results", "6")
	})

	ctx := store.WithTenantID(context.Background(), tenantID)
	baseHash := methods.resolveConfigForContext(ctx).Hash()
	params := map[string]any{
		"raw":      `{"tools":{"web":{"provider_order":["duckduckgo","exa","brave"],"exa":{"enabled":true,"api_key":"***","max_results":7},"brave":{"enabled":true,"api_key":"tenant-brave","max_results":3},"duckduckgo":{"enabled":false,"max_results":6}}}}`,
		"baseHash": baseHash,
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	req := &protocol.RequestFrame{
		ID:     "req-tenant",
		Method: protocol.MethodConfigPatch,
		Params: rawParams,
	}

	methods.handlePatch(ctx, &gateway.Client{}, req)

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := loaded.Tools.Web.ProviderOrder; len(got) != 4 || got[0] != "brave" || got[1] != "tavily" || got[2] != "exa" || got[3] != "duckduckgo" {
		t.Fatalf("disk provider_order = %v, want original master order", got)
	}
	if methods.cfg.Tools.Web.Brave.Enabled {
		t.Fatal("master in-memory config should not be mutated by tenant patch")
	}

	if got := secretsStore.values[store.MasterTenantID]["tools.web.exa.api_key"]; got != "master-exa" {
		t.Fatalf("master exa key = %q, want master-exa", got)
	}
	if got := secretsStore.values[tenantID]["tools.web.exa.api_key"]; got != "tenant-exa" {
		t.Fatalf("tenant exa key = %q, want tenant-exa", got)
	}
	if got := secretsStore.values[tenantID]["tools.web.brave.api_key"]; got != "tenant-brave" {
		t.Fatalf("tenant brave key = %q, want tenant-brave", got)
	}

	resolved := methods.resolveConfigForContext(ctx)
	wantOrder := []string{"exa", "brave", "tavily", "duckduckgo"}
	for i, want := range wantOrder {
		if resolved.Tools.Web.ProviderOrder[i] != want {
			t.Fatalf("tenant provider_order[%d] = %q, want %q (full=%v)", i, resolved.Tools.Web.ProviderOrder[i], want, resolved.Tools.Web.ProviderOrder)
		}
	}
	if !resolved.Tools.Web.DuckDuckGo.Enabled {
		t.Fatal("tenant-resolved DDG fallback should stay enabled")
	}
	if resolved.Tools.Web.DuckDuckGo.MaxResults != 6 {
		t.Fatalf("tenant DDG max_results = %d, want 6", resolved.Tools.Web.DuckDuckGo.MaxResults)
	}
}
