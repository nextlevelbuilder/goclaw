package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// subscribeShellDenyGroupsReload wires pub/sub so global shell deny-group
// toggles applied via the /config page take effect without a process restart.
// Extracted from runLifecycle to make the dispatch path unit-testable
// (the regression coverage missing from the original PR #1005 attempt).
func subscribeShellDenyGroupsReload(msgBus *bus.MessageBus, toolsReg *tools.Registry) {
	msgBus.Subscribe("shell-deny-groups-config-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		snapshot := updatedCfg.Clone()
		execTool, ok := toolsReg.Get("exec")
		if !ok {
			return
		}
		et, ok := execTool.(*tools.ExecTool)
		if !ok {
			return
		}
		et.SetGlobalShellDenyGroups(snapshot.Tools.ShellDenyGroups)
		et.SetCommandKeywordAllowlist(snapshot.Tools.CommandKeywordAllowlist)
		slog.Info("shell deny groups reloaded via pub/sub",
			"groups", len(snapshot.Tools.ShellDenyGroups),
			"command_keyword_allowlist_rules", len(snapshot.Tools.CommandKeywordAllowlist),
		)
	})
}

func subscribeProviderShellDenyGroupsReload(msgBus *bus.MessageBus, providerReg *providers.Registry, provStore store.ProviderStore, mcpStore store.MCPServerStore) {
	if msgBus == nil || providerReg == nil {
		return
	}
	msgBus.Subscribe("shell-deny-provider-policy-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		reloadShellDenyProviderPolicies(providerReg, provStore, mcpStore, updatedCfg)
	})
}

func reloadShellDenyProviderPolicies(providerReg *providers.Registry, provStore store.ProviderStore, mcpStore store.MCPServerStore, cfg *config.Config) {
	if providerReg == nil || cfg == nil {
		return
	}
	snapshot := cfg.Clone()
	registerClaudeCLIFromConfig(providerReg, snapshot)
	if snapshot.Providers.ACP.Binary != "" {
		registerACPFromConfig(providerReg, snapshot.Providers.ACP, snapshot.ShellDenyGroupsSnapshot())
	}
	if provStore == nil {
		return
	}
	dbProviders, err := provStore.ListAllProviders(context.Background())
	if err != nil {
		slog.Warn("shell deny provider policy reload: failed to load providers from DB", "error", err)
		return
	}
	gatewayAddr := loopbackAddr(snapshot.Gateway.Host, snapshot.Gateway.Port)
	for _, p := range dbProviders {
		if !p.Enabled {
			continue
		}
		switch p.ProviderType {
		case store.ProviderClaudeCLI:
			registerClaudeCLIFromDB(providerReg, p, gatewayAddr, snapshot.Gateway.Token, mcpStore, snapshot)
		case store.ProviderACP:
			registerACPFromDB(providerReg, p, snapshot.ShellDenyGroupsSnapshot())
		}
	}
}
