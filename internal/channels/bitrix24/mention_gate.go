package bitrix24

import (
	"os"
	"strings"
	"sync"
)

// defaultRequireMentionConnectors is the built-in set of Open Channel
// connectors that require a bot @mention before reply. Group-style
// connectors — where multiple external customers share a single Open
// Channel chat — MUST gate, otherwise the bot would spam every customer
// turn (including customer-to-customer chatter that doesn't address the
// bot at all). 1-to-1 connectors (Facebook Messenger, Zalo OA, …) do NOT
// need gating: every customer message is addressed to the bot by
// construction, so requiring a mention there would drop legitimate
// traffic that customers have no syntactic way to produce.
//
// "synity_zalo_personal" is currently the only known group-style Openline
// connector in this deployment — Zalo personal pools several customers
// into one Bitrix Open Channel chat. Operators can extend or override
// the set via the BITRIX24_REQUIRE_MENTION_CONNECTORS env var (see
// requireMentionConnectorSet below).
var defaultRequireMentionConnectors = []string{"synity_zalo_personal"}

var (
	requireMentionOnce sync.Once
	requireMentionSet  map[string]bool
)

// requireMentionConnectorSet returns the connector codes (the leading
// token of CHAT_ENTITY_ID — e.g. "synity_zalo_personal" out of
// "synity_zalo_personal|20|grp|960") that require a bot @mention.
//
// Resolution: env BITRIX24_REQUIRE_MENTION_CONNECTORS overrides the
// hardcoded default. The env value is a comma-separated list of codes;
// empty entries and whitespace are tolerated. An empty env value falls
// back to the default set — set the var to a single comma or a sentinel
// like "none" if you genuinely want to disable gating entirely.
//
// The set is parsed once and cached for the process lifetime. Operators
// who edit the env need to restart the container (same operational
// model as the other BITRIX24_* env vars in docker-compose.yml).
func requireMentionConnectorSet() map[string]bool {
	requireMentionOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("BITRIX24_REQUIRE_MENTION_CONNECTORS"))
		set := map[string]bool{}
		if raw == "" {
			for _, code := range defaultRequireMentionConnectors {
				set[code] = true
			}
		} else {
			for _, code := range strings.Split(raw, ",") {
				if code = strings.TrimSpace(code); code != "" {
					set[code] = true
				}
			}
		}
		requireMentionSet = set
	})
	return requireMentionSet
}

// connectorCodeFromEntityID extracts the leading token of CHAT_ENTITY_ID.
// Open Channel payloads ship CHAT_ENTITY_ID as a pipe-separated string
// like "synity_zalo_personal|20|grp|960"; the first segment names the
// connector ("synity_zalo_personal" here). Returns "" when the field is
// empty or missing — callers treat empty as "unknown connector, do not
// gate" so legacy Openline events without an entity id default to the
// non-gated path.
func connectorCodeFromEntityID(entityID string) string {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return ""
	}
	if i := strings.IndexByte(entityID, '|'); i >= 0 {
		return entityID[:i]
	}
	return entityID
}

// shouldRequireMentionForOpenline decides whether an Open Channel inbound
// message must @-mention the bot before the agent picks it up.
//
// Policy (matches reviewed business intent):
//
//   - Internal staff in an Openline chat (IS_CONNECTOR=N) → require mention.
//     Staff use the operator UI and CAN @-mention; without the gate the bot
//     would jump into every operator-to-operator side conversation in the
//     same Open Channel session.
//
//   - External customer (IS_CONNECTOR=Y) on a group-style connector listed
//     in requireMentionConnectorSet → require mention. Customers cannot
//     produce a mention from outside (no syntax for it in Zalo), so this is
//     effectively "drop traffic that the upstream connector didn't tag as
//     bot-addressed".
//
//   - External customer on any other connector (FB Messenger, Zalo OA, the
//     long tail of 1-to-1 connectors, or any payload without a recognised
//     CHAT_ENTITY_ID) → do NOT require mention. Every message is addressed
//     to the bot by construction; gating would silently drop legitimate
//     customer turns.
func shouldRequireMentionForOpenline(fromConnector bool, chatEntityID string) bool {
	if !fromConnector {
		return true
	}
	code := connectorCodeFromEntityID(chatEntityID)
	if code == "" {
		return false
	}
	return requireMentionConnectorSet()[code]
}
