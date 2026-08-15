package hooks

import "github.com/nextlevelbuilder/goclaw/internal/edition"

// HookEditionPolicy is a pure-function policy gate applied at BOTH config
// validation time AND at dispatcher fire time (defense-in-depth).
//
// Rule: the `command` handler executes arbitrary shell; it is only safe in
// the Lite edition where the operator physically owns the host. On Standard
// (multi-tenant managed hosting), `command` is blocked across every scope
// (C2 drop decision). `http` and `prompt` are allowed on both editions.
type HookEditionPolicy struct{}

// Allow returns (ok, reason). reason is only set when ok=false.
// reason is a stable string suitable for audit rows and i18n key lookup.
func (HookEditionPolicy) Allow(ht HandlerType, _ Scope, ed edition.Edition) (bool, string) {
	switch ht {
	case HandlerHTTP, HandlerPrompt, HandlerScript:
		// Script hooks run in a sandboxed goja runtime — no shell escape surface.
		// Allowed on every edition for tenant admin + operator authors alike.
		return true, ""
	case HandlerCommand:
		if ed.Name == edition.Lite.Name {
			return true, ""
		}
		return false, "hook.command_disabled_standard"
	default:
		return false, "hook.unknown_handler_type"
	}
}
