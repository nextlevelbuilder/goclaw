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
)

// Values for MetaKeyVisibility. Stored as strings (not bool) so callers
// can distinguish "explicitly public" from "absent" if needed in future.
const (
	VisibilityWhisper = "whisper"
	VisibilityPublic  = "public"
)
