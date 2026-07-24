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
		{"aider", "api_key", ""},     // provider not wired yet
		{"", "api_key", ""},          // empty provider
	}
	for _, c := range cases {
		if got := injectForConnection(c.provider, c.credType); got != c.want {
			t.Errorf("injectForConnection(%q, %q) = %q, want %q", c.provider, c.credType, got, c.want)
		}
	}
}
