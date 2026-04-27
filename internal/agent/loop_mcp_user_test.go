package agent

import "testing"

// TestResolveActorUserID locks the actor-vs-context user-id resolution semantics
// that gate per-user MCP credential lookup (and other per-actor resources).
//
// The bug this helper fixes: cmd/gateway_consumer_normal.go rewrites UserID to
// a group-scope composite ("group:<channel>:<chatID>") for shared memory in
// group chats, but the Bitrix24 lazy provisioner saves MCPUserCredentials
// rows keyed by the real external user id (= SenderID). Looking them up with
// the group composite always missed → MCP tools silently absent in group
// chats. resolveActorUserID restores SenderID as the lookup key for groups
// while leaving DM behaviour (UserID == SenderID) unchanged.
func TestResolveActorUserID(t *testing.T) {
	cases := []struct {
		name     string
		userID   string
		senderID string
		peerKind string
		want     string
	}{
		// DM: gateway leaves UserID == SenderID. Helper is a no-op.
		{
			name:     "dm_returns_user_id_unchanged",
			userID:   "99",
			senderID: "99",
			peerKind: "direct",
			want:     "99",
		},
		// Group: gateway overrides UserID with group composite for shared
		// memory. Helper must recover SenderID for actor-scoped lookups.
		{
			name:     "group_overrides_to_sender",
			userID:   "group:bitrix-synity:chat4838",
			senderID: "99",
			peerKind: "group",
			want:     "99",
		},
		// Discord guild composite ("guild:<id>:user:<sender>") is also a
		// group peer — fall back to SenderID for credential lookup.
		{
			name:     "discord_guild_overrides_to_sender",
			userID:   "guild:1234:user:5678",
			senderID: "5678",
			peerKind: "group",
			want:     "5678",
		},
		// Synthetic / system senders (ticker, notification) carry empty
		// SenderID. No per-user credentials exist for them — fall back to
		// UserID so the lookup still uses a sensible key.
		{
			name:     "group_with_empty_sender_falls_back_to_user_id",
			userID:   "group:bitrix-synity:chat4838",
			senderID: "",
			peerKind: "group",
			want:     "group:bitrix-synity:chat4838",
		},
		// Empty peer_kind defaults to direct semantics.
		{
			name:     "empty_peer_kind_treated_as_direct",
			userID:   "99",
			senderID: "99",
			peerKind: "",
			want:     "99",
		},
		// Future channel using a peer_kind we don't recognize must NOT be
		// treated as group automatically — DM semantics are the safer
		// default (no override).
		{
			name:     "unknown_peer_kind_does_not_override",
			userID:   "99",
			senderID: "42",
			peerKind: "channel",
			want:     "99",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveActorUserID(tc.userID, tc.senderID, tc.peerKind)
			if got != tc.want {
				t.Errorf("resolveActorUserID(%q, %q, %q) = %q; want %q",
					tc.userID, tc.senderID, tc.peerKind, got, tc.want)
			}
		})
	}
}
