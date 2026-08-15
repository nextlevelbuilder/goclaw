package cmd

import (
	"context"
	"maps"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type captureSystemConfigStore struct {
	data map[string]string
}

func (s *captureSystemConfigStore) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

func (s *captureSystemConfigStore) Set(_ context.Context, key, value string) error {
	s.data[key] = value
	return nil
}

func (s *captureSystemConfigStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *captureSystemConfigStore) List(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.data))
	maps.Copy(out, s.data)
	return out, nil
}

func TestSeedConfigForContextPersistsZeroInboundDebounce(t *testing.T) {
	t.Parallel()

	sc := &captureSystemConfigStore{data: map[string]string{}}
	cfg := config.Default()
	cfg.Gateway.InboundDebounceMs = 0

	seedConfigForContext(store.WithTenantID(context.Background(), store.MasterTenantID), sc, cfg, false)

	if got := sc.data["gateway.inbound_debounce_ms"]; got != "0" {
		t.Fatalf("gateway.inbound_debounce_ms = %q, want 0", got)
	}
}

func TestSeedConfigForContextDoesNotCreateSkillUploadTenantOverride(t *testing.T) {
	t.Parallel()

	sc := &captureSystemConfigStore{data: map[string]string{}}
	cfg := config.Default()
	cfg.Skills.MaxUploadSizeMB = 64

	seedConfigForContext(store.WithTenantID(context.Background(), store.MasterTenantID), sc, cfg, false)

	if _, ok := sc.data[config.SkillMaxUploadSizeSystemConfigKey]; ok {
		t.Fatalf("%s should not be seeded; missing key lets SKILL.md frontmatter override global config", config.SkillMaxUploadSizeSystemConfigKey)
	}
}
