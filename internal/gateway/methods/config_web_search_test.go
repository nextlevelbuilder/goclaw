package methods

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
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

	methods.saveSecretsToStore(context.Background(), cfg)

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

	methods.saveSecretsToStore(context.Background(), cfg)

	if got := store.values["tools.web.exa.api_key"]; got != "old-exa" {
		t.Fatalf("exa key = %q, want old-exa", got)
	}
	if got := store.values["tools.web.brave.api_key"]; got != "old-brave" {
		t.Fatalf("brave key = %q, want old-brave", got)
	}
}
