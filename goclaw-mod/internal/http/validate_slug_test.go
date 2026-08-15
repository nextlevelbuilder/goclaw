package http

import "testing"

// TestIsValidSlug covers the slug predicate used by agent_key, skill slug,
// provider name, and MCP server name validation. The slug format is the
// router cache's canonical anchor — the cache splits on the last colon for
// exact-segment invalidation, so the predicate MUST reject any character
// that would collide with that split (notably `:`).
func TestIsValidSlug(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple lowercase", "agent", true},
		{"with hyphen", "goctech-leader", true},
		{"with digits", "agent-42", true},
		{"single char", "a", true},
		{"starts with digit", "1-agent", true},

		{"empty", "", false},
		{"uppercase rejected", "Agent", false},
		{"starts with hyphen", "-agent", false},
		{"ends with hyphen", "agent-", false},
		{"colon rejected", "weird:key", false},
		{"slash rejected", "weird/key", false},
		{"whitespace rejected", "weird key", false},
		{"dot rejected", "weird.key", false},
		{"tab rejected", "weird\tkey", false},
		{"underscore rejected", "weird_key", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidSlug(tc.input); got != tc.want {
				t.Errorf("isValidSlug(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
