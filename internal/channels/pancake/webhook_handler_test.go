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
		// TikTok variants (tt=Livestream AIO, ttm=Business Messaging, tts=TikTok Shop)
		{"tiktok_livestream", "tt_12345678_987654321", "tt_12345678"},
		{"tiktok_messaging", "ttm_12345678_987654321", "ttm_12345678"},
		{"tiktok_shop", "tts_12345678_987654321", "tts_12345678"},
		{"tiktok_system_2_segments", "tt_12345678", "tt_12345678"}, // system event w/o sender
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
