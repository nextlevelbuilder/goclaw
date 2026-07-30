package http

import "testing"

func TestInjectForConnection(t *testing.T) {
	cases := []struct {
		provider string
		credType string
		want     string
	}{
		{"claude_code", "api_key", "env:ANTHROPIC_API_KEY"},
		{"Claude", "api_key", "env:ANTHROPIC_API_KEY"}, // case-insensitive
		{"claudecode", "oauth", "env:CLAUDE_CODE_OAUTH_TOKEN"},
		{"claude_code", "bogus", ""}, // unknown cred type
		// aider used to expect "" with the comment "provider not wired yet". It IS
		// wired now: the mapping derives non-Claude providers from the adapter
		// layer, so a CLI goclaw can delegate to can also hold a credential.
		// See TestInjectForConnectionCoversEveryDelegatableProvider.
		{"aider", "api_key", "env:ANTHROPIC_API_KEY"},
		{"codex", "oauth", ""}, // only Claude Code has an OAuth login flow
		{"", "api_key", ""},    // empty provider resolves to nothing
	}
	for _, c := range cases {
		if got := injectForConnection(c.provider, c.credType); got != c.want {
			t.Errorf("injectForConnection(%q, %q) = %q, want %q", c.provider, c.credType, got, c.want)
		}
	}
}
