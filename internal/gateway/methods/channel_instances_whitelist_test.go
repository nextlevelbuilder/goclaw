package methods

import "testing"

// TestIsValidChannelType_WS guards the WebSocket-side whitelist.
// Pre-existing bug surfaced by this test: facebook + pancake were missing
// from the WS list while the HTTP list at internal/http/channel_instances.go
// already accepts them.
func TestIsValidChannelType_WS(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"telegram":      true,
		"discord":       true,
		"slack":         true,
		"whatsapp":      true,
		"zalo_oa":       true,
		"zalo_bot":      true,
		"zalo_personal": true,
		"feishu":        true,
		"facebook":      true,
		"pancake":       true,
		"unknown":       false,
		"":              false,
		"zalo":          false,
		"zalo_oauth":    false,
	}

	for ct, want := range cases {
		if got := isValidChannelType(ct); got != want {
			t.Errorf("isValidChannelType(%q) = %v, want %v", ct, got, want)
		}
	}
}
