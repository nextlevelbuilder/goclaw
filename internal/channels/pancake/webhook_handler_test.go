package pancake

import "testing"

// TestResolvePageIDFromConvID verifies platform-prefix-aware pageID extraction.
// This test will FAIL until Phase 2 introduces the resolvePageIDFromConvID helper.
func TestResolvePageIDFromConvID(t *testing.T) {
	cases := []struct {
		name   string
		convID string
		want   string
	}{
		{"facebook_numeric", "123456_789012", "123456"},
		{"shopee_prefixed", "spo_25409726_109139680425439630", "spo_25409726"},
		{"shopee_system_2_segments", "spo_25409726", "spo_25409726"}, // M2: system event w/o sender — return as-is
		{"empty_input", "", ""},
		{"no_underscore", "abcdef", ""},
		{"prefix_only_no_underscore", "spo", ""}, // regression: prefix-only without underscore
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePageIDFromConvID(tc.convID); got != tc.want {
				t.Fatalf("resolvePageIDFromConvID(%q) = %q, want %q",
					tc.convID, got, tc.want)
			}
		})
	}
}
