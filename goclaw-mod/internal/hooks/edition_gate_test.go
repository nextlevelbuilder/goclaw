package hooks_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// TestEditionGate covers the full matrix (handlerType × scope × edition).
// Per plan C2 drop: `command` is Lite-only across all scopes.
// `http` and `prompt` are allowed on both editions.
func TestEditionGate(t *testing.T) {
	var policy hooks.HookEditionPolicy

	cases := []struct {
		name        string
		handlerType hooks.HandlerType
		scope       hooks.Scope
		ed          edition.Edition
		wantAllow   bool
	}{
		// command on Lite — always allowed.
		{"command/global/lite", hooks.HandlerCommand, hooks.ScopeGlobal, edition.Lite, true},
		{"command/tenant/lite", hooks.HandlerCommand, hooks.ScopeTenant, edition.Lite, true},
		{"command/agent/lite", hooks.HandlerCommand, hooks.ScopeAgent, edition.Lite, true},

		// command on Standard — blocked at every scope (C2 drop).
		{"command/global/standard", hooks.HandlerCommand, hooks.ScopeGlobal, edition.Standard, false},
		{"command/tenant/standard", hooks.HandlerCommand, hooks.ScopeTenant, edition.Standard, false},
		{"command/agent/standard", hooks.HandlerCommand, hooks.ScopeAgent, edition.Standard, false},

		// http — allowed on both editions, all scopes.
		{"http/global/lite", hooks.HandlerHTTP, hooks.ScopeGlobal, edition.Lite, true},
		{"http/tenant/standard", hooks.HandlerHTTP, hooks.ScopeTenant, edition.Standard, true},
		{"http/agent/standard", hooks.HandlerHTTP, hooks.ScopeAgent, edition.Standard, true},

		// prompt — allowed on both editions, all scopes.
		{"prompt/global/lite", hooks.HandlerPrompt, hooks.ScopeGlobal, edition.Lite, true},
		{"prompt/tenant/standard", hooks.HandlerPrompt, hooks.ScopeTenant, edition.Standard, true},
		{"prompt/agent/lite", hooks.HandlerPrompt, hooks.ScopeAgent, edition.Lite, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allow, reason := policy.Allow(tc.handlerType, tc.scope, tc.ed)
			if allow != tc.wantAllow {
				t.Errorf("Allow(%v,%v,%v) = %v, want %v (reason=%q)",
					tc.handlerType, tc.scope, tc.ed.Name, allow, tc.wantAllow, reason)
			}
			// When denied, a non-empty reason string MUST be returned.
			if !allow && reason == "" {
				t.Errorf("Allow(%v,%v,%v) denied but reason is empty",
					tc.handlerType, tc.scope, tc.ed.Name)
			}
		})
	}
}

// TestEditionGateUnknownHandler rejects unknown handler types defensively.
func TestEditionGateUnknownHandler(t *testing.T) {
	var policy hooks.HookEditionPolicy
	allow, reason := policy.Allow(hooks.HandlerType("webhook"), hooks.ScopeTenant, edition.Lite)
	if allow {
		t.Errorf("unknown handler should not be allowed; got allow=true reason=%q", reason)
	}
}
