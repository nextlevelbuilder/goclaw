package bitrix24

// Metadata keys used to propagate Bitrix24-specific context from inbound
// events through bus.InboundMessage → bus.OutboundMessage → Send().
// Pattern follows existing keys (bitrix_address_user_id, bitrix_chat_entity_*,
// bitrix_dialog_id, etc.). Defining as constants gives a single source of
// truth that handle.go, gateway_consumer_normal.go, and send.go can share.
const (
	// MetaKeyVisibility distinguishes whisper (internal-only) vs public
	// (forwarded to external connector) messages. Set on inbound by
	// handle.go from EventParams.IsHiddenMessage. Read on outbound by
	// Send() to route through imbot.message.add with SKIP_CONNECTOR=Y
	// (whisper) or imbot.v2.Chat.Message.send (public).
	MetaKeyVisibility = "bitrix_visibility"

	// MetaKeyMessageID is the MESSAGE_ID of the inbound message that
	// triggered this exchange. Set on inbound by handle.go. Read on
	// outbound v2 path → set as fields.replyId so the Bitrix UI shows
	// the bot's reply linked to the original.
	//
	// NOTE: this key was already in use before this refactor; the
	// constant just documents it. Do not rename without grepping for
	// the literal "bitrix_message_id" across the repo.
	MetaKeyMessageID = "bitrix_message_id"

	// MetaKeySenderPrefix carries the openline sender tag echo extracted from an
	// inbound openline message by handle.go. For the 3-token connector layout it
	// is "#msgId" (msgId only); for the legacy single-number layout it is the
	// canonical "[name] #msgId"; for name-only it is "[name]". Forwarded by
	// gateway_consumer_normal.go and read on outbound by Send(), which prepends
	// it to the reply so the Bitrix Open Channel connector can route the answer
	// back to the right external message. Empty / absent for plain Bitrix24 chats.
	MetaKeySenderPrefix = "bitrix_sender_prefix"

	// MetaKeyParticipantUserID carries a per-participant synthetic user ID built
	// from the external person's uid parsed out of the connector's 3-token sender
	// tag ("[Name] #uid #msgId"). Shape: "openlines:{channelInstance}:{chatID}:{uid}".
	// Set by handle.go ONLY when FromIsConnector=true and a uid was present. Read
	// by gateway_consumer_normal.go to scope per-person USER.md / memory / seeding
	// instead of the group-level fallback. Empty / absent for legacy single-number,
	// name-only, or non-connector (operator) messages — those degrade safely to the
	// group-level userID.
	MetaKeyParticipantUserID = "participant_user_id"
)

// Values for MetaKeyVisibility. Stored as strings (not bool) so callers
// can distinguish "explicitly public" from "absent" if needed in future.
const (
	VisibilityWhisper = "whisper"
	VisibilityPublic  = "public"
)
