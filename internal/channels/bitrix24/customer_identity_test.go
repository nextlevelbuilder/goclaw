package bitrix24

import "testing"

// TestConnectorCode pins the parse of CHAT_ENTITY_ID's leading connector
// token. The "|"-separated shape ("synity_zalo_personal|20|grp|960") comes
// straight off the Open Channel webhook payload — see live capture in
// docs/journals/260620-zalo-openline-per-participant-identity.md.
func TestConnectorCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"synity_zalo_personal|20|zpersonal_grp_X|960", "synity_zalo_personal"},
		{"synity_zalo_personal", "synity_zalo_personal"},
		{"", ""},
		{"|20|x", ""},
	}
	for _, tc := range cases {
		if got := connectorCode(tc.in); got != tc.want {
			t.Errorf("connectorCode(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestPersonHandle covers the registry: known connector + uid → prefixed
// handle, everything else → "" so caller falls back to legacy senderID.
// Unknown connectors deliberately produce "" (not a guess) — new connectors
// (Facebook, Zalo OA, …) plug in by adding a row to personHandlePrefixes
// once their payload shape is verified from a real webhook capture.
func TestPersonHandle(t *testing.T) {
	cases := []struct {
		name         string
		chatEntityID string
		uid          string
		want         string
	}{
		{
			name:         "zalo personal connector returns zalo handle",
			chatEntityID: "synity_zalo_personal|20|zpersonal_grp_X|960",
			uid:          "3393421773556008363",
			want:         "zalo:3393421773556008363",
		},
		{
			name:         "unknown connector falls back to empty",
			chatEntityID: "facebook|10|some_page|960",
			uid:          "psid999",
			want:         "",
		},
		{
			name:         "empty uid returns empty even on known connector",
			chatEntityID: "synity_zalo_personal|20|x|960",
			uid:          "",
			want:         "",
		},
		{
			name:         "empty CHAT_ENTITY_ID returns empty",
			chatEntityID: "",
			uid:          "3393421773556008363",
			want:         "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := personHandle(tc.chatEntityID, tc.uid); got != tc.want {
				t.Errorf("personHandle(%q, %q) = %q; want %q", tc.chatEntityID, tc.uid, got, tc.want)
			}
		})
	}
}
