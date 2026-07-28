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

func TestSeedConfigForContextDoesNotPersistRecoveryInterval(t *testing.T) {
	t.Parallel()

	// gateway.task_recovery_interval_sec is process-wide, startup-only and
	// restart-required (Phase 7 closure item 6). The tenant seed/sync path must
	// not seed or upsert it: a tenant-persisted value would be read back by the
	// dynamic per-tenant apply path and wrongly treated as a live tenant setting.
	// Assert it is absent in BOTH onlyMissing modes — the false (seed) and true
	// (sync/patch) branches both flow through seedConfigForContext.
	for _, onlyMissing := range []bool{false, true} {
		sc := &captureSystemConfigStore{data: map[string]string{}}
		cfg := config.Default()
		cfg.Gateway.TaskRecoveryIntervalSec = 120

		seedConfigForContext(store.WithTenantID(context.Background(), store.MasterTenantID), sc, cfg, onlyMissing)

		if _, ok := sc.data["gateway.task_recovery_interval_sec"]; ok {
			t.Fatalf("onlyMissing=%v: gateway.task_recovery_interval_sec must not be tenant-seeded (startup-only, restart-required)", onlyMissing)
		}
	}
}

func TestSeedConfigForContextDoesNotOverwriteExistingRecoveryInterval(t *testing.T) {
	t.Parallel()

	// An existing persisted value (e.g. an older build that seeded it) must be
	// left untouched by the tenant sync path — seedConfigForContext neither seeds
	// nor rewrites the key, preserving upgrade compatibility for the startup
	// master-config reader (Phase 7 closure item 6.2).
	sc := &captureSystemConfigStore{data: map[string]string{
		"gateway.task_recovery_interval_sec": "45",
	}}
	cfg := config.Default()
	cfg.Gateway.TaskRecoveryIntervalSec = 300

	seedConfigForContext(store.WithTenantID(context.Background(), store.MasterTenantID), sc, cfg, false)

	if got := sc.data["gateway.task_recovery_interval_sec"]; got != "45" {
		t.Fatalf("gateway.task_recovery_interval_sec = %q, want 45 (seed path must not rewrite an existing value)", got)
	}
}

func TestSeedConfigForContextClearsTeamWorkClassifierOverrides(t *testing.T) {
	t.Parallel()

	sc := &captureSystemConfigStore{data: map[string]string{
		"gateway.team_work_classify_provider": "old-provider",
		"gateway.team_work_classify_model":    "old-model",
	}}
	cfg := config.Default()
	cfg.Gateway.TeamWorkClassifyProvider = ""
	cfg.Gateway.TeamWorkClassifyModel = ""

	seedConfigForContext(store.WithTenantID(context.Background(), store.MasterTenantID), sc, cfg, false)

	if got := sc.data["gateway.team_work_classify_provider"]; got != "" {
		t.Fatalf("gateway.team_work_classify_provider = %q, want empty", got)
	}
	if got := sc.data["gateway.team_work_classify_model"]; got != "" {
		t.Fatalf("gateway.team_work_classify_model = %q, want empty", got)
	}
}
