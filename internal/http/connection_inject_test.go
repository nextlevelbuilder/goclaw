package http

import (
	"encoding/json"
	"testing"
)

// A provider that goclaw can DELEGATE to must also be able to hold a credential.
// Before these, only claude_code was mapped, so a Codex/Aider/Gemini connection
// could be created — binary present in the sandbox image, argv known — and then
// rejected with "does not support a api_key credential", making it unusable.
func TestInjectForConnectionCoversEveryDelegatableProvider(t *testing.T) {
	cases := []struct {
		provider string
		credType string
		cfg      json.RawMessage
		want     string
	}{
		// Claude Code stays explicit: it accepts both kinds and they differ.
		{"claude_code", "api_key", nil, "env:ANTHROPIC_API_KEY"},
		{"claude_code", "oauth", nil, "env:CLAUDE_CODE_OAUTH_TOKEN"},
		{"Claude", "oauth", nil, "env:CLAUDE_CODE_OAUTH_TOKEN"}, // alias, case-insensitive

		// Derived from the adapter layer — these are the ones that used to fail.
		{"codex", "api_key", nil, "env:CODEX_API_KEY"},
		{"aider", "api_key", nil, "env:ANTHROPIC_API_KEY"},
		{"gemini_cli", "api_key", nil, "env:GEMINI_API_KEY"},

		// generic gets its var from config only — nothing else can know it.
		{"generic", "api_key", json.RawMessage(`{"binary":"mycli","task_args":["run","{{task}}"],"output":"text","cred_env":["MYCLI_KEY"]}`), "env:MYCLI_KEY"},

		// A per-connection override must win over the built-in default.
		{"codex", "api_key", json.RawMessage(`{"cred_env":["MY_OVERRIDE"]}`), "env:MY_OVERRIDE"},

		// Unsupported combinations must stay empty so callers 400 rather than
		// storing a credential that can never be delivered.
		{"codex", "oauth", nil, ""},      // only Claude Code has an OAuth flow
		{"generic", "api_key", nil, ""},  // no config → nothing to derive from
		{"nonsense", "api_key", nil, ""}, // unknown, and no config to make it generic
	}

	for _, tc := range cases {
		got := injectForConnectionCfg(tc.provider, tc.credType, tc.cfg)
		if got != tc.want {
			t.Errorf("injectForConnectionCfg(%q, %q, cfg=%s) = %q, want %q",
				tc.provider, tc.credType, string(tc.cfg), got, tc.want)
		}
	}
}

// The OAuth login preflight uses the config-less form; it must still gate on
// Claude Code only, so we never start a subscription login for a key-only CLI.
func TestInjectForConnectionOAuthIsClaudeOnly(t *testing.T) {
	if injectForConnection("claude_code", "oauth") == "" {
		t.Error("claude_code must support oauth")
	}
	for _, p := range []string{"codex", "aider", "gemini_cli", "generic"} {
		if got := injectForConnection(p, "oauth"); got != "" {
			t.Errorf("%s must not advertise oauth support, got %q", p, got)
		}
	}
}
