package max

// API types for Max Messenger Bot API.
//
// Authoritative documentation:
//   - Overview:     https://dev.max.ru/docs-api
//   - User:         https://dev.max.ru/docs-api/objects/User
//   - Chat:         https://dev.max.ru/docs-api/objects/Chat
//   - Message:      https://dev.max.ru/docs-api/objects/Message
//   - Update:       https://dev.max.ru/docs-api/objects/Update
//   - NewMessageBody: https://dev.max.ru/docs-api/objects/NewMessageBody
//
// All numeric IDs are int64 per docs ("integer <int64>").
// Timestamps are Unix milliseconds per docs.

// User represents a User or Bot in Max.
// Returned by GET /me (with is_bot=true) and embedded as Message.sender.
type User struct {
	UserID           int64        `json:"user_id"`
	FirstName        string       `json:"first_name"`
	LastName         string       `json:"last_name,omitempty"`
	Username         string       `json:"username,omitempty"`
	IsBot            bool         `json:"is_bot"`
	LastActivityTime int64        `json:"last_activity_time,omitempty"`
	Name             string       `json:"name,omitempty"` // deprecated per docs
	Description      string       `json:"description,omitempty"`
	AvatarURL        string       `json:"avatar_url,omitempty"`
	FullAvatarURL    string       `json:"full_avatar_url,omitempty"`
	Commands         []BotCommand `json:"commands,omitempty"`
}

// BotCommand is a menu command supported by a bot.
type BotCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ChatType enum per docs/Chat object.
// Note: docs only document "chat" formally, but the API also returns "dialog"
// for DM threads — established via dialog_with_user field presence and
// participants_count == 2.
const (
	ChatTypeChat   = "chat"   // group chat
	ChatTypeDialog = "dialog" // direct message thread
)

// ChatStatus enum.
const (
	ChatStatusActive  = "active"
	ChatStatusRemoved = "removed"
	ChatStatusLeft    = "left"
	ChatStatusClosed  = "closed"
)

// Chat represents a chat room (group or dialog).
type Chat struct {
	ChatID            int64    `json:"chat_id"`
	Type              string   `json:"type"`
	Status            string   `json:"status,omitempty"`
	Title             string   `json:"title,omitempty"`
	Icon              *Image   `json:"icon,omitempty"`
	LastEventTime     int64    `json:"last_event_time,omitempty"`
	ParticipantsCount int32    `json:"participants_count,omitempty"`
	OwnerID           *int64   `json:"owner_id,omitempty"`
	IsPublic          bool     `json:"is_public,omitempty"`
	Link              string   `json:"link,omitempty"`
	Description       string   `json:"description,omitempty"`
	DialogWithUser    *User    `json:"dialog_with_user,omitempty"`
	ChatMessageID     string   `json:"chat_message_id,omitempty"`
	PinnedMessage     *Message `json:"pinned_message,omitempty"`
}

// IsDialog returns true if this chat represents a 1-1 conversation.
// Uses multiple signals because docs show both "chat" enum value AND
// reference dialog semantics; we treat presence of dialog_with_user
// or participants_count==2 as authoritative.
func (c *Chat) IsDialog() bool {
	return c.Type == ChatTypeDialog ||
		c.DialogWithUser != nil ||
		(c.Type == "" && c.ParticipantsCount == 2)
}

// Image is a generic image reference (avatar, chat icon).
type Image struct {
	URL string `json:"url,omitempty"`
}

// Recipient identifies who receives a message — a user (DM) or a chat (group).
//
// IMPORTANT: In real Max API responses, BOTH UserID and ChatID are populated
// for direct messages (UserID = bot user_id, ChatID = dialog thread ID).
// The authoritative discriminator is ChatType.
type Recipient struct {
	UserID   int64  `json:"user_id,omitempty"`
	ChatID   int64  `json:"chat_id,omitempty"`
	ChatType string `json:"chat_type,omitempty"` // "dialog" | "chat"
}

// IsDialog returns true if this recipient identifies a 1:1 conversation.
// Uses ChatType as the authoritative signal because UserID is also populated
// for DMs (it's the bot's user_id, not a discriminator).
func (r *Recipient) IsDialog() bool {
	return r.ChatType == ChatTypeDialog
}

// MessageBody holds the content of a Message.
type MessageBody struct {
	MID         string       `json:"mid"` // message ID
	Seq         int64        `json:"seq"` // sequence in chat
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Markup      []Markup     `json:"markup,omitempty"` // text formatting markup
}

// Markup describes text formatting per inline ranges.
type Markup struct {
	Type   string `json:"type"` // "strong" | "emphasized" | etc.
	From   int    `json:"from"` // start char offset
	Length int    `json:"length"`
	URL    string `json:"url,omitempty"`     // for "link" type
	UserID int64  `json:"user_id,omitempty"` // for "user_mention"
}

// Attachment types per Max API.
// Each kind has different Payload shape — use Type to dispatch.
const (
	AttachmentTypeImage          = "image"
	AttachmentTypeVideo          = "video"
	AttachmentTypeAudio          = "audio"
	AttachmentTypeFile           = "file"
	AttachmentTypeSticker        = "sticker"
	AttachmentTypeContact        = "contact"
	AttachmentTypeShare          = "share"
	AttachmentTypeLocation       = "location"
	AttachmentTypeInlineKeyboard = "inline_keyboard"
)

// Attachment is a polymorphic media/UI attachment on a Message.
// Inspect Type, then Payload.* fields appropriate for that type.
type Attachment struct {
	Type    string            `json:"type"`
	Payload AttachmentPayload `json:"payload,omitempty"`
}

// AttachmentPayload is a union of fields across all attachment types.
// JSON unmarshalling fills only the fields relevant to the actual type.
type AttachmentPayload struct {
	// Common fields (image, video, audio, file)
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"` // upload reference for outbound

	// Image-specific
	PhotoID int64 `json:"photo_id,omitempty"`

	// Video-specific
	VideoToken string `json:"video_token,omitempty"`

	// File-specific
	FileID   int64  `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`

	// Audio-specific
	Duration int `json:"duration,omitempty"`

	// Sticker-specific
	Code string `json:"code,omitempty"`

	// Contact-specific (request_contact button responses)
	VcfInfo string `json:"vcf_info,omitempty"`
	MaxInfo *User  `json:"max_info,omitempty"`
	Hash    string `json:"hash,omitempty"` // HMAC-SHA256(access_token, vcf_info)

	// Share-specific (share button)
	ShareURL string `json:"share_url,omitempty"`

	// Location-specific
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`

	// Inline keyboard-specific (outbound only)
	Buttons [][]InlineButton `json:"buttons,omitempty"`
}

// InlineButton is a single button in an inline keyboard.
type InlineButton struct {
	Type    string `json:"type"` // "callback" | "link" | "request_contact" | etc.
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"` // for callback / clipboard
	URL     string `json:"url,omitempty"`     // for link
	Intent  string `json:"intent,omitempty"`  // "default" | "positive" | "negative"
}

// LinkedMessage represents a forwarded or replied-to message.
type LinkedMessage struct {
	Type    string       `json:"type"` // "reply" | "forward"
	Sender  *User        `json:"sender,omitempty"`
	Message *MessageBody `json:"message,omitempty"`
	ChatID  int64        `json:"chat_id,omitempty"`
}

// Message is a chat message — both inbound (from updates) and outbound (send response).
//
// Note on field naming: the Max API returns the inner content under JSON key
// "message" (not "body" as suggested by some docs sections). We keep the Go
// field name `Body` for readability but bind to "message" via json tag.
type Message struct {
	Sender    *User          `json:"sender,omitempty"`
	Recipient *Recipient     `json:"recipient,omitempty"`
	Timestamp int64          `json:"timestamp"`
	Link      *LinkedMessage `json:"link,omitempty"`
	Body      *MessageBody   `json:"message,omitempty"`
	URL       string         `json:"url,omitempty"`
}

// UpdateType enum per docs.
const (
	UpdateTypeMessageCreated   = "message_created"
	UpdateTypeMessageEdited    = "message_edited"
	UpdateTypeMessageRemoved   = "message_removed"
	UpdateTypeMessageCallback  = "message_callback"
	UpdateTypeBotAdded         = "bot_added"
	UpdateTypeBotRemoved       = "bot_removed"
	UpdateTypeUserAdded        = "user_added"
	UpdateTypeUserRemoved      = "user_removed"
	UpdateTypeChatTitleChanged = "chat_title_changed"
)

// Update is a server-pushed event (via long polling or webhook).
// Sub-fields populated depend on UpdateType.
type Update struct {
	UpdateType string `json:"update_type"`
	Timestamp  int64  `json:"timestamp"`
	UserLocale string `json:"user_locale,omitempty"`

	// For message_created / message_edited
	Message *Message `json:"message,omitempty"`

	// For message_callback
	Callback *Callback `json:"callback,omitempty"`

	// For message_removed
	MessageID string `json:"message_id,omitempty"`
	ChatID    int64  `json:"chat_id,omitempty"`
	UserID    int64  `json:"user_id,omitempty"`

	// For bot_added / bot_removed / user_added / user_removed
	Chat      *Chat `json:"chat,omitempty"`
	User      *User `json:"user,omitempty"`
	IsChannel bool  `json:"is_channel,omitempty"`
}

// Callback is a button-click event.
type Callback struct {
	Timestamp  int64  `json:"timestamp"`
	CallbackID string `json:"callback_id"`
	Payload    string `json:"payload"`
	User       *User  `json:"user,omitempty"`
}

// UpdatesResponse is the GET /updates body.
type UpdatesResponse struct {
	Updates []Update `json:"updates"`
	Marker  *int64   `json:"marker"` // pointer to next page; nil = no more
}

// SendMessageRequest is the POST /messages JSON body.
type SendMessageRequest struct {
	Text        string          `json:"text,omitempty"`
	Attachments []Attachment    `json:"attachments,omitempty"`
	Link        *NewMessageLink `json:"link,omitempty"`
	Notify      *bool           `json:"notify,omitempty"`
	Format      string          `json:"format,omitempty"` // "markdown" | "html" | ""
}

// NewMessageLink references a message to reply to or forward.
type NewMessageLink struct {
	Type      string `json:"type"` // "reply" | "forward"
	MessageID string `json:"message_id,omitempty"`
}

// SendMessageResponse is the POST /messages response.
//
// Per live API observation, the response includes top-level convenience
// fields (chat_id, recipient_id, message_id) in addition to the nested
// Message object. We capture them all — message_id is needed for
// subsequent EditMessage / DeleteMessage calls.
type SendMessageResponse struct {
	Message     Message `json:"message"`
	ChatID      int64   `json:"chat_id,omitempty"`
	RecipientID int64   `json:"recipient_id,omitempty"`
	MessageID   string  `json:"message_id,omitempty"`
}

// EditMessageRequest is the PUT /messages JSON body.
type EditMessageRequest struct {
	Text        string          `json:"text,omitempty"`
	Attachments []Attachment    `json:"attachments,omitempty"`
	Link        *NewMessageLink `json:"link,omitempty"`
	Notify      *bool           `json:"notify,omitempty"`
	Format      string          `json:"format,omitempty"`
}

// SubscriptionRequest is the POST /subscriptions JSON body.
type SubscriptionRequest struct {
	URL         string   `json:"url"`
	UpdateTypes []string `json:"update_types,omitempty"`
	Version     string   `json:"version,omitempty"`
}

// APIError is the standard error body returned by Max API.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return "max api: " + e.Code + ": " + e.Message
	}
	return "max api: " + e.Code
}
