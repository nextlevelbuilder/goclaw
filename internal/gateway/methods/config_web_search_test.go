package methods

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/config"
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
		"raw": `{"tools":{"web":{"provider_order":["duckduckgo","exa","brave","exa"],"exa":{"enabled":true,"api_key":"***","max_results":7},"brave":{"enabled":true,"api_key":"new-brave","max_results":3}}}}`,
		"baseHash": cfg.Hash(),
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
