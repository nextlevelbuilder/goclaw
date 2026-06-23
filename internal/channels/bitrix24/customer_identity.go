package bitrix24

import "strings"

// connectorCode returns the connector identifier prefix from CHAT_ENTITY_ID.
// Open Channel sessions ship CHAT_ENTITY_ID shaped as "<connector>|<line>|<grp>|<proxyUserID>"
// (e.g. "synity_zalo_personal|20|zpersonal_grp_X|960"). Anything before the
// first "|" is the connector code; an empty / missing field returns "".
func connectorCode(chatEntityID string) string {
	if i := strings.IndexByte(chatEntityID, '|'); i >= 0 {
		return chatEntityID[:i]
	}
	return chatEntityID
}

// personHandlePrefixes maps a connector code to the prefix used for the
// cross-context person handle stored in channel_contacts.user_id. The handle
// shape is "<prefix>:<realUID>" — e.g. "zalo:3393421773556008363" — and is
// what auto-merge keys on so the same external person across multiple
// chats/groups (same connector) collapses into one tenant_user.
//
// Connectors not listed here are unknown to us yet: we leave the handle empty
// so the row keeps the legacy behavior (user_id mirrors sender_id). New
// connectors (Facebook, Zalo OA, …) plug in by adding a line here once their
// payload shape is verified from a real webhook capture.
var personHandlePrefixes = map[string]string{
	"synity_zalo_personal": "zalo",
}

// personHandle returns the cross-context person handle for an Openline
// connector customer. uid is the external person's stable id parsed from the
// connector's sender tag. Returns "" when the connector is unknown or uid is
// empty — caller should fall back to the legacy mirror-sender-id behavior.
func personHandle(chatEntityID, uid string) string {
	if uid == "" {
		return ""
	}
	prefix := personHandlePrefixes[connectorCode(chatEntityID)]
	if prefix == "" {
		return ""
	}
	return prefix + ":" + uid
}
