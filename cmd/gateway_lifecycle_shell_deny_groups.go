package cmd

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
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
		execTool, ok := toolsReg.Get("exec")
		if !ok {
			return
		}
		et, ok := execTool.(*tools.ExecTool)
		if !ok {
			return
		}
		et.SetGlobalShellDenyGroups(updatedCfg.Tools.ShellDenyGroups)
		slog.Info("shell deny groups reloaded via pub/sub", "groups", len(updatedCfg.Tools.ShellDenyGroups))
	})
}
