package bitrix24

import (
	"sync"
	"testing"
)

// resetRequireMentionCache lets each test observe the env override path —
// the package-level sync.Once otherwise locks in whatever the FIRST caller
// in the process saw. Tests that touch BITRIX24_REQUIRE_MENTION_CONNECTORS
// MUST call this before reading the set, and again at cleanup so a later
// test doesn't inherit a polluted cache.
func resetRequireMentionCache() {
	requireMentionOnce = sync.Once{}
	requireMentionSet = nil
}

func TestConnectorCodeFromEntityID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Real-shape payload — what the Bitrix Open Channel webhook actually
		// ships for the Synity Zalo personal connector.
		{"synity_zalo_personal|20|grp|960", "synity_zalo_personal"},
		// Hypothetical future Facebook Open Channel payload — verifies the
		// helper doesn't require any specific connector name to work.
		{"facebook|34|36305652112415023|1074", "facebook"},
		// Token with no pipe at all — return the whole string so the caller
		// can still look it up in the whitelist.
		{"plain_token", "plain_token"},
		// Surrounding whitespace from sloppy upstream serialization gets
		// trimmed before the split — otherwise we'd never match.
		{"  spaced|x|y  ", "spaced"},
		// Empty / pipe-leading cases collapse to "" so the gate falls into
		// the "unknown connector → don't gate" branch.
		{"", ""},
		{"|leading|pipe", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := connectorCodeFromEntityID(tc.in); got != tc.want {
				t.Errorf("connectorCodeFromEntityID(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRequireMentionConnectorSet_DefaultWhenEnvUnset pins the safety
// default: when operators haven't configured BITRIX24_REQUIRE_MENTION_CONNECTORS
// the gate still trips for the one connector we currently know is
// group-style (Zalo personal). Without this default a fresh deployment
// would silently reply to every Zalo customer turn.
func TestRequireMentionConnectorSet_DefaultWhenEnvUnset(t *testing.T) {
	resetRequireMentionCache()
	defer resetRequireMentionCache()
	t.Setenv("BITRIX24_REQUIRE_MENTION_CONNECTORS", "")

	set := requireMentionConnectorSet()
	if !set["synity_zalo_personal"] {
		t.Error("default set must include synity_zalo_personal")
	}
	if set["facebook"] {
		t.Error("default set must not include facebook (1-to-1 connector)")
	}
}

// TestRequireMentionConnectorSet_EnvOverride is the whole point of the
// env-driven design — operators can add a future group-style connector
// without redeploying the binary. Once "zalo_group" gets shipped this
// is the runtime knob that turns the gate on for it.
func TestRequireMentionConnectorSet_EnvOverride(t *testing.T) {
	resetRequireMentionCache()
	defer resetRequireMentionCache()
	t.Setenv("BITRIX24_REQUIRE_MENTION_CONNECTORS", "synity_zalo_personal, zalo_group ,  ")

	set := requireMentionConnectorSet()
	if !set["synity_zalo_personal"] {
		t.Error("override must keep synity_zalo_personal")
	}
	if !set["zalo_group"] {
		t.Error("override must add zalo_group")
	}
	// Whitespace + trailing empty entry from the comma split must NOT
	// register a phantom "" key — that would gate every payload with an
	// empty CHAT_ENTITY_ID, which we explicitly want to leave un-gated.
	if set[""] {
		t.Error("override must not register an empty-string key")
	}
}

// TestShouldRequireMentionForOpenline is the policy matrix in one place.
// Each row corresponds to a decided business case from the spec review,
// so a regression on any row is a regression on a deliberate decision.
func TestShouldRequireMentionForOpenline(t *testing.T) {
	resetRequireMentionCache()
	defer resetRequireMentionCache()
	t.Setenv("BITRIX24_REQUIRE_MENTION_CONNECTORS", "")

	cases := []struct {
		name          string
		fromConnector bool
		chatEntityID  string
		want          bool
	}{
		// Staff (IS_CONNECTOR=N) ALWAYS gate — they can @-mention and we
		// don't want the bot jumping into operator-to-operator chatter.
		{"staff in zalo personal still gates", false, "synity_zalo_personal|20|grp|960", true},
		{"staff in facebook still gates", false, "facebook|34|x|960", true},
		{"staff without entity id still gates", false, "", true},
		// Customer (IS_CONNECTOR=Y) on the whitelisted connector → gate.
		// Multiple customers share one Zalo personal Open Channel chat, so
		// every untagged turn would otherwise spam them.
		{"customer on zalo personal gates", true, "synity_zalo_personal|20|grp|960", true},
		// Customer on a 1-to-1 connector → DO NOT gate. Every message is
		// addressed to the bot; gating would drop legitimate traffic.
		{"customer on facebook does not gate", true, "facebook|34|x|1074", false},
		{"customer on zalo_oa does not gate", true, "zalo_oa|1|x|1", false},
		// Customer with empty CHAT_ENTITY_ID → DO NOT gate. We can't
		// distinguish the source, and the safe default for the long tail
		// of legacy payloads is "let it through" rather than "silently
		// drop and look broken".
		{"customer without entity id does not gate", true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRequireMentionForOpenline(tc.fromConnector, tc.chatEntityID); got != tc.want {
				t.Errorf("shouldRequireMentionForOpenline(connector=%v, entity=%q) = %v; want %v",
					tc.fromConnector, tc.chatEntityID, got, tc.want)
			}
		})
	}
}
