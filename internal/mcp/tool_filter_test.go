package mcp

import "testing"

func TestIsToolAllowed(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		allow    []string
		deny     []string
		expected bool
	}{
		{"no filter allows everything", "search", nil, nil, true},
		{"empty allow allows everything", "search", []string{}, []string{}, true},
		{"tool in allow", "search", []string{"search", "list"}, nil, true},
		{"tool not in allow", "delete", []string{"search", "list"}, nil, false},
		{"tool in deny", "delete", nil, []string{"delete"}, false},
		{"deny wins over allow", "delete", []string{"delete"}, []string{"delete"}, false},
		{"allow + deny coexist (allowed)", "search", []string{"search"}, []string{"delete"}, true},
		{"allow + deny coexist (denied by allow)", "list", []string{"search"}, []string{"delete"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsToolAllowed(tc.tool, tc.allow, tc.deny)
			if got != tc.expected {
				t.Errorf("IsToolAllowed(%q, allow=%v, deny=%v) = %v, want %v",
					tc.tool, tc.allow, tc.deny, got, tc.expected)
			}
		})
	}
}
