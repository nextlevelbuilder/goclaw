package agent

import "testing"

// TestResolveActorUserID locks the actor-vs-context user-id resolution semantics
// that gate per-user MCP credential lookup (and other per-actor resources).
//
// Two bugs this helper fixes:
//
//  1. Group chats: gateway consumer rewrites UserID to a group-scope composite
//     ("group:<channel>:<chatID>") for shared memory. The Bitrix24 provisioner
//     stores MCPUserCredentials keyed by the real external user id (= SenderID).
//     Lookup with group composite always missed → MCP tools silently absent.
//
//  2. DM with merged contact (C1): gateway consumer rewrites DM UserID to the
//     tenant_user UUID when ContactCollector.ResolveTenantUserID succeeds.
//     Provisioner still stores by SenderID. Lookup with UUID misses → MCP
//     tools fail in DMs after contact merge.
//
// resolveActorUserID accepts channelType so Bitrix24 always recovers SenderID
// (covers both rewrite cases). Other channels retain group-only recovery.
func TestResolveActorUserID(t *testing.T) {
	cases := []struct {
		name        string
		userID      string
		senderID    string
		peerKind    string
		channelType string
		want        string
	}{
		// DM unmerged: UserID == SenderID. No rewrite happened.
		{
			name:        "dm_returns_user_id_unchanged",
			userID:      "99",
			senderID:    "99",
			peerKind:    "direct",
			channelType: "",
			want:        "99",
		},
		// Group: gateway overrides UserID with group composite for shared
		// memory. Helper must recover SenderID for actor-scoped lookups.
		{
			name:        "group_overrides_to_sender",
			userID:      "group:bitrix-synity:chat4838",
			senderID:    "99",
			peerKind:    "group",
			channelType: "",
			want:        "99",
		},
		// Discord guild composite ("guild:<id>:user:<sender>") is also a
		// group peer — fall back to SenderID for credential lookup.
		{
			name:        "discord_guild_overrides_to_sender",
			userID:      "guild:1234:user:5678",
			senderID:    "5678",
			peerKind:    "group",
			channelType: "",
			want:        "5678",
		},
		// Synthetic / system senders (ticker, notification) carry empty
		// SenderID. No per-user credentials exist for them — fall back to
		// UserID so the lookup still uses a sensible key.
		{
			name:        "group_with_empty_sender_falls_back_to_user_id",
			userID:      "group:bitrix-synity:chat4838",
			senderID:    "",
			peerKind:    "group",
			channelType: "",
			want:        "group:bitrix-synity:chat4838",
		},
		// Empty peer_kind defaults to direct semantics.
		{
			name:        "empty_peer_kind_treated_as_direct",
			userID:      "99",
			senderID:    "99",
			peerKind:    "",
			channelType: "",
			want:        "99",
		},
		// Future channel using a peer_kind we don't recognize must NOT be
		// treated as group automatically — DM semantics are the safer
		// default (no override).
		{
			name:        "unknown_peer_kind_does_not_override",
			userID:      "99",
			senderID:    "42",
			peerKind:    "channel",
			channelType: "",
			want:        "99",
		},

		// ── Bitrix24-specific cases (C1 fix) ───────────────────────────

		// Bitrix24 DM, sender NOT merged: UserID == SenderID. Helper returns
		// SenderID (which is identical) — same outcome either way.
		{
			name:        "bitrix24_dm_unmerged_uses_sender",
			userID:      "62",
			senderID:    "62",
			peerKind:    "direct",
			channelType: "bitrix24",
			want:        "62",
		},
		// Bitrix24 DM, sender MERGED to tenant_user (C1 bug): consumer
		// rewrites UserID to tenant_user UUID. Provisioner stored creds
		// by SenderID. Helper must return SenderID so lookup hits.
		{
			name:        "bitrix24_dm_merged_uses_sender_not_uuid",
			userID:      "uuid-abc-def-0123",
			senderID:    "62",
			peerKind:    "direct",
			channelType: "bitrix24",
			want:        "62",
		},
		// Bitrix24 group: same recovery as generic group, channelType
		// discriminator does no harm.
		{
			name:        "bitrix24_group_uses_sender",
			userID:      "group:bitrix-tamgiac:chat4686",
			senderID:    "62",
			peerKind:    "group",
			channelType: "bitrix24",
			want:        "62",
		},
		// Bitrix24 synthetic event (system/ticker) with no sender: fall
		// back to UserID. No creds exist anyway.
		{
			name:        "bitrix24_synthetic_no_sender_falls_back",
			userID:      "system",
			senderID:    "",
			peerKind:    "direct",
			channelType: "bitrix24",
			want:        "system",
		},

		// ── Other channel backward compat ──────────────────────────────

		// Telegram DM (no channelType match): keep original behavior.
		// Telegram doesn't provision per-user creds today; if it did, the
		// consumer's UserID rewrite for merged contacts would still apply
		// and Telegram support would be added here when introduced.
		{
			name:        "telegram_dm_unchanged",
			userID:      "user-456",
			senderID:    "789",
			peerKind:    "direct",
			channelType: "telegram",
			want:        "user-456",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveActorUserID(tc.userID, tc.senderID, tc.peerKind, tc.channelType)
			if got != tc.want {
				t.Errorf("resolveActorUserID(%q, %q, %q, %q) = %q; want %q",
					tc.userID, tc.senderID, tc.peerKind, tc.channelType, got, tc.want)
			}
		})
	}
}
