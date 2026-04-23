package agent

import "testing"

func TestIsSilentReply(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Exact matches.
		{"exact", "NO_REPLY", true},
		{"with whitespace", "  NO_REPLY  ", true},
		{"with newlines", "\nNO_REPLY\n", true},
		// Decorative variants — the bug report.
		{"trailing underscore", "NO_REPLY_", true},
		{"double trailing underscore", "NO_REPLY__", true},
		{"leading underscore", "_NO_REPLY", true},
		{"both underscores", "_NO_REPLY_", true},
		{"trailing dot", "NO_REPLY.", true},
		{"trailing bang", "NO_REPLY!", true},
		{"double-quoted", `"NO_REPLY"`, true},
		{"single-quoted", "'NO_REPLY'", true},
		{"backticked", "`NO_REPLY`", true},
		{"markdown bold", "**NO_REPLY**", true},
		{"parenthesized", "(NO_REPLY)", true},
		// Case-insensitive.
		{"lowercase", "no_reply", true},
		{"mixed case", "No_Reply", true},
		// Silent — token + explanation (user intent: prefix-match, divergent from upstream).
		{"prefix + space + content", "NO_REPLY hello", true},
		{"prefix + colon + content", "NO_REPLY: offline", true},
		{"prefix + because", "NO_REPLY because user is away", true},
		// NOT silent — token glued to another word, or not at start.
		{"embedded word", "NO_REPLYING", false},
		{"trailing after content", "Here you go. NO_REPLY", false},
		{"token mid-sentence", "Hello NO_REPLY world", false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"unrelated text", "no reply needed", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSilentReply(c.in); got != c.want {
				t.Errorf("IsSilentReply(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
