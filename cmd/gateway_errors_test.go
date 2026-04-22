package cmd

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// TestIsExternalChannel ensures the whitelist matches actual channel type
// constants — a mismatch here silently leaks internal error strings to end
// users (e.g. "zalo" vs "zalo_oa"/"zalo_personal"). Always compare against
// channels.Type* constants, never string literals.
func TestIsExternalChannel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		channelType string
		want        bool
	}{
		// Public-facing platforms — errors must be suppressed.
		{"facebook", channels.TypeFacebook, true},
		{"telegram", channels.TypeTelegram, true},
		{"discord", channels.TypeDiscord, true},
		{"feishu", channels.TypeFeishu, true},
		{"whatsapp", channels.TypeWhatsApp, true},
		{"zalo_oa", channels.TypeZaloOA, true},
		{"zalo_personal", channels.TypeZaloPersonal, true},
		{"pancake", channels.TypePancake, true},
		{"slack", channels.TypeSlack, true},

		// Internal / unknown channel types — errors must still surface.
		{"ws", "ws", false},
		{"empty", "", false},
		{"unknown", "myplatform", false},
		// "line" is a Pancake sub-platform, not a top-level channel type.
		{"line_not_top_level", "line", false},
		// Legacy short "zalo" string must NOT match — real constants are zalo_oa / zalo_personal.
		{"zalo_short_form", "zalo", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExternalChannel(tc.channelType); got != tc.want {
				t.Fatalf("isExternalChannel(%q) = %v, want %v", tc.channelType, got, tc.want)
			}
		})
	}
}
