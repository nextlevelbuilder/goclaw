package hooks

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DisableLegacyCommandHooks turns off any enabled command-typed hook rows
// when running on Standard edition. No-op on Lite. Idempotent: second call
// finds nothing to disable.
//
// Context: Phase 01 removed the UI affordance for creating `command` hooks on
// Standard edition; Phase 03's edition gate rejects new writes at the WS RPC
// layer. But Standard deployments that existed BEFORE Wave 1 already have
// command-hook rows in the DB, and those rows keep firing via the dispatcher.
// This helper is the one-time-per-boot runtime migration that flips them off.
//
// Why not a SQL migration?
//   - Lite runs on SQLite and must not be affected even when sharing code
//   - Edition is a runtime flag, not a schema property
//   - Fresh Standard DBs that restore a Lite backup may reintroduce command
//     rows; re-running on each boot handles that safely
//
// Called once at startup from cmd/gateway_managed.go after migrations and the
// builtin seed, BEFORE HTTP/WS listeners accept traffic.
func DisableLegacyCommandHooks(ctx context.Context, hs HookStore, ed edition.Edition) (int, error) {
	if ed.Name != edition.Standard.Name {
		return 0, nil
	}
	if hs == nil {
		return 0, nil
	}

	// Reach all tenants' rows. store.WithOwnerRole does NOT exist — see
	// phase-04 red-team notes. Use WithRole explicitly.
	ctx = store.WithRole(ctx, store.RoleOwner)

	enabled := true
	rows, err := hs.List(ctx, ListFilter{Enabled: &enabled})
	if err != nil {
		return 0, err
	}

	n := 0
	for _, r := range rows {
		if r.HandlerType != HandlerCommand {
			continue
		}
		if r.Source == SourceBuiltin {
			// Defensive: Phase 04/05 only seed HandlerScript, but guard anyway
			// so a future accidental builtin command row wouldn't get disabled.
			continue
		}
		if err := hs.Update(ctx, r.ID, map[string]any{"enabled": false}); err != nil {
			slog.Warn("hooks.command_hook_auto_disable_failed",
				"hook_id", r.ID, "tenant_id", r.TenantID, "err", err)
			continue
		}
		slog.Warn("hooks.command_hook_auto_disabled",
			"hook_id", r.ID,
			"tenant_id", r.TenantID,
			"reason", "command_handler_disabled_on_standard_edition")
		n++
	}
	if n > 0 {
		slog.Info("hooks.command_migration_complete", "disabled_count", n)
	}
	return n, nil
}
