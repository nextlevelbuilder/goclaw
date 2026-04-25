package cmd

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
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
