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

	// ---- Chat entity context keys (populated from EntityContext.ToMeta) ----
	//
	// Emitted by entity_context.go on top of the raw bitrix_chat_entity_type /
	// bitrix_chat_entity_id fields already set by handle.go. Only present when
	// the source Bitrix payload carries a decodable value, so agents / tools can
	// treat "key missing" as "not applicable" without extra flags.

	// MetaKeyChatTitle is the human-readable chat name Bitrix ships in
	// data[PARAMS][CHAT_TITLE]. Populated for every chat that has a title
	// (group chats, Open Channel sessions, tasks, workgroups). Absent on
	// 1-1 DMs where Bitrix does not attach a title.
	MetaKeyChatTitle = "bitrix_chat_title"

	// MetaKeyChatType mirrors data[PARAMS][CHAT_TYPE] — the single-letter
	// Bitrix chat classifier ("L" openline, "C" group/CRM, "X" tasks, "B"
	// collab, "P" private, ...). More precise than ChatEntityType for
	// disambiguating surfaces.
	MetaKeyChatType = "bitrix_chat_type"

	// Openline (LINES) session fields, decoded from CHAT_ENTITY_ID
	// ("<connector>|<line>|<external_uid>|<bitrix_user_id>") and
	// CHAT_ENTITY_DATA_1 (10-token session state).
	MetaKeyOLConnectorCode  = "bitrix_ol_connector_code"
	MetaKeyOLLineID         = "bitrix_ol_line_id"
	MetaKeyOLExternalUID    = "bitrix_ol_external_uid"
	MetaKeyOLBitrixUserID   = "bitrix_ol_bitrix_user_id"
	MetaKeyOLLineConfigID   = "bitrix_ol_line_config_id"
	MetaKeyOLSessionStarted = "bitrix_ol_session_started_at"
	MetaKeyOLActiveCRMType  = "bitrix_ol_active_crm_type"
	MetaKeyOLActiveCRMID    = "bitrix_ol_active_crm_id"

	// CRM linkage — sourced from CHAT_ENTITY_DATA_2 on Openline chats
	// (schema "LEAD|<id>|COMPANY|<id>|CONTACT|<id>|DEAL|<id>"), or from
	// CHAT_ENTITY_ID on native CRM-integrated chats (2-token
	// "<TYPE>|<id>"). Only the ids that are non-zero are emitted.
	MetaKeyCRMLeadID    = "bitrix_crm_lead_id"
	MetaKeyCRMCompanyID = "bitrix_crm_company_id"
	MetaKeyCRMContactID = "bitrix_crm_contact_id"
	MetaKeyCRMDealID    = "bitrix_crm_deal_id"

	// Native CRM chat scalars — from CHAT_ENTITY_ID like "CONTACT|1780".
	// Kept alongside the type-specific *_id keys so agents can either look
	// up the entity id directly or dispatch on the type token.
	MetaKeyCRMEntityType = "bitrix_crm_type"
	MetaKeyCRMEntityID   = "bitrix_crm_id"

	// Single-token ENTITY_ID cases (Task / Workgroup / Mail).
	MetaKeyTaskID      = "bitrix_task_id"
	MetaKeyWorkgroupID = "bitrix_workgroup_id"
	MetaKeyMailID      = "bitrix_mail_id"
)

// Values for MetaKeyVisibility. Stored as strings (not bool) so callers
// can distinguish "explicitly public" from "absent" if needed in future.
const (
	VisibilityWhisper = "whisper"
	VisibilityPublic  = "public"
)
