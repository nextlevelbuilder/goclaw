package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestShellDenyGroupsConfigReload_UpdatesGlobal asserts the pub/sub subscriber
// dispatches a TopicConfigChanged event into ExecTool.SetGlobalShellDenyGroups —
// the regression coverage that the original PR #1005 was missing.
func TestShellDenyGroupsConfigReload_UpdatesGlobal(t *testing.T) {
	msgBus := bus.New()
	defer msgBus.Unsubscribe("shell-deny-groups-config-reload")

	toolsReg := tools.NewRegistry()
	execTool := tools.NewExecTool("/tmp", false)
	toolsReg.Register(execTool)

	subscribeShellDenyGroupsReload(msgBus, toolsReg)

	msgBus.Broadcast(bus.Event{
		Name: bus.TopicConfigChanged,
		Payload: &config.Config{
			Tools: config.ToolsConfig{
				ShellDenyGroups: map[string]bool{"package_install": true},
			},
		},
	})

	got := execTool.EffectiveDenyGroupsForTest(context.Background())
	if v, ok := got["package_install"]; !ok || v != true {
		t.Fatalf("expected pub/sub to set global package_install=true, got %v", got)
	}
	rules := execTool.CommandKeywordAllowlistForTest()
	if len(rules) != 0 {
		t.Fatalf("expected empty command keyword allowlist, got %v", rules)
	}
}

func TestShellDenyGroupsConfigReload_UpdatesCommandKeywordAllowlist(t *testing.T) {
	msgBus := bus.New()
	defer msgBus.Unsubscribe("shell-deny-groups-config-reload")

	toolsReg := tools.NewRegistry()
	execTool := tools.NewExecTool("/tmp", false)
	toolsReg.Register(execTool)

	subscribeShellDenyGroupsReload(msgBus, toolsReg)

	msgBus.Broadcast(bus.Event{
		Name: bus.TopicConfigChanged,
		Payload: &config.Config{
			Tools: config.ToolsConfig{
				CommandKeywordAllowlist: []config.CommandKeywordAllowlistRule{
					{ID: "github-content", Command: "gh", Args: []string{"--body"}, Keywords: []string{"secret"}},
				},
			},
		},
	})

	rules := execTool.CommandKeywordAllowlistForTest()
	if len(rules) != 1 || rules[0].ID != "github-content" {
		t.Fatalf("expected command keyword allowlist to reload, got %v", rules)
	}
}

// TestShellDenyGroupsConfigReload_IgnoresOtherEvents: subscriber must guard
// on event.Name and ignore non-TopicConfigChanged broadcasts.
func TestShellDenyGroupsConfigReload_IgnoresOtherEvents(t *testing.T) {
	msgBus := bus.New()
	defer msgBus.Unsubscribe("shell-deny-groups-config-reload")

	toolsReg := tools.NewRegistry()
	execTool := tools.NewExecTool("/tmp", false)
	execTool.SetGlobalShellDenyGroups(map[string]bool{"package_install": false}) // baseline
	toolsReg.Register(execTool)

	subscribeShellDenyGroupsReload(msgBus, toolsReg)

	msgBus.Broadcast(bus.Event{
		Name: bus.TopicAgentDeleted,
		Payload: &config.Config{
			Tools: config.ToolsConfig{
				ShellDenyGroups: map[string]bool{"package_install": true},
			},
		},
	})

	got := execTool.EffectiveDenyGroupsForTest(context.Background())
	if v := got["package_install"]; v != false {
		t.Fatalf("expected non-config event to be ignored; package_install changed to %v", v)
	}
}

// TestShellDenyGroupsConfigReload_IgnoresWrongPayload: subscriber must
// type-assert payload to *config.Config and skip mismatched payloads.
func TestShellDenyGroupsConfigReload_IgnoresWrongPayload(t *testing.T) {
	msgBus := bus.New()
	defer msgBus.Unsubscribe("shell-deny-groups-config-reload")

	toolsReg := tools.NewRegistry()
	execTool := tools.NewExecTool("/tmp", false)
	execTool.SetGlobalShellDenyGroups(map[string]bool{"package_install": false})
	toolsReg.Register(execTool)

	subscribeShellDenyGroupsReload(msgBus, toolsReg)

	msgBus.Broadcast(bus.Event{
		Name:    bus.TopicConfigChanged,
		Payload: "not-a-config-pointer",
	})

	got := execTool.EffectiveDenyGroupsForTest(context.Background())
	if v := got["package_install"]; v != false {
		t.Fatalf("expected wrong-payload event to be ignored; package_install changed to %v", v)
	}
}

func TestShellDenyGroupsConfigReload_ReplacesConfigClaudeCLIProvider(t *testing.T) {
	msgBus := bus.New()
	defer msgBus.Unsubscribe("shell-deny-provider-policy-reload")

	providerReg := providers.NewRegistry(store.TenantIDFromContext)
	defer providerReg.Close()

	initial := config.Default()
	initial.Providers.ClaudeCLI.CLIPath = "claude"
	initial.Tools.ShellDenyGroups = map[string]bool{"package_install": true}
	reloadShellDenyProviderPolicies(providerReg, nil, nil, initial)

	before, err := providerReg.Get(context.Background(), "claude-cli")
	if err != nil {
		t.Fatal(err)
	}
	beforeCLI, ok := before.(*providers.ClaudeCLIProvider)
	if !ok {
		t.Fatalf("expected ClaudeCLI provider, got %T", before)
	}

	updated := config.Default()
	updated.Providers.ClaudeCLI.CLIPath = "claude"
	updated.Tools.ShellDenyGroups = map[string]bool{"package_install": false}
	subscribeProviderShellDenyGroupsReload(msgBus, providerReg, nil, nil)
	msgBus.Broadcast(bus.Event{Name: bus.TopicConfigChanged, Payload: updated})

	after, err := providerReg.Get(context.Background(), "claude-cli")
	if err != nil {
		t.Fatal(err)
	}
	afterCLI, ok := after.(*providers.ClaudeCLIProvider)
	if !ok {
		t.Fatalf("expected ClaudeCLI provider after reload, got %T", after)
	}
	if beforeCLI == afterCLI {
		t.Fatal("expected config change to replace Claude CLI provider runtime")
	}
}

func TestShellDenyGroupsConfigReload_ReplacesDBClaudeCLIProvider(t *testing.T) {
	providerReg := providers.NewRegistry(store.TenantIDFromContext)
	defer providerReg.Close()

	tenantID := uuid.New()
	binary := writeTestExecutable(t)
	provStore := &shellDenyGroupsProviderStore{providers: []store.LLMProviderData{
		{
			TenantID:     tenantID,
			Name:         "tenant-claude",
			ProviderType: store.ProviderClaudeCLI,
			APIBase:      binary,
			Enabled:      true,
		},
	}}

	initial := config.Default()
	initial.Tools.ShellDenyGroups = map[string]bool{"package_install": true}
	reloadShellDenyProviderPolicies(providerReg, provStore, nil, initial)

	before, err := providerReg.GetForTenant(tenantID, "tenant-claude")
	if err != nil {
		t.Fatal(err)
	}
	beforeCLI, ok := before.(*providers.ClaudeCLIProvider)
	if !ok {
		t.Fatalf("expected ClaudeCLI provider, got %T", before)
	}

	updated := config.Default()
	updated.Tools.ShellDenyGroups = map[string]bool{"package_install": false}
	reloadShellDenyProviderPolicies(providerReg, provStore, nil, updated)

	after, err := providerReg.GetForTenant(tenantID, "tenant-claude")
	if err != nil {
		t.Fatal(err)
	}
	afterCLI, ok := after.(*providers.ClaudeCLIProvider)
	if !ok {
		t.Fatalf("expected ClaudeCLI provider after reload, got %T", after)
	}
	if beforeCLI == afterCLI {
		t.Fatal("expected config change to replace DB Claude CLI provider runtime")
	}
}

func TestConfiguredShellDenyPatternsDropsDisabledPackageInstall(t *testing.T) {
	cfg := config.Default()
	cfg.Tools.ShellDenyGroups = map[string]bool{"package_install": false}

	patterns := configuredShellDenyPatterns(cfg)

	if matchesAny(patterns, "pip install requests") {
		t.Fatal("package_install=false should remove package-install deny patterns")
	}
	if !matchesAny(patterns, "env") {
		t.Fatal("unrelated default deny patterns should remain active")
	}
}

type shellDenyGroupsProviderStore struct {
	providers []store.LLMProviderData
}

func (s *shellDenyGroupsProviderStore) CreateProvider(context.Context, *store.LLMProviderData) error {
	return errors.New("not implemented")
}

func (s *shellDenyGroupsProviderStore) GetProvider(context.Context, uuid.UUID) (*store.LLMProviderData, error) {
	return nil, errors.New("not implemented")
}

func (s *shellDenyGroupsProviderStore) GetProviderByName(context.Context, string) (*store.LLMProviderData, error) {
	return nil, errors.New("not implemented")
}

func (s *shellDenyGroupsProviderStore) ListProviders(context.Context) ([]store.LLMProviderData, error) {
	return s.providers, nil
}

func (s *shellDenyGroupsProviderStore) ListAllProviders(context.Context) ([]store.LLMProviderData, error) {
	return s.providers, nil
}

func (s *shellDenyGroupsProviderStore) UpdateProvider(context.Context, uuid.UUID, map[string]any) error {
	return errors.New("not implemented")
}

func (s *shellDenyGroupsProviderStore) DeleteProvider(context.Context, uuid.UUID) error {
	return errors.New("not implemented")
}

func writeTestExecutable(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "claude-test")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return binary
}

func matchesAny(patterns []*regexp.Regexp, command string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}
