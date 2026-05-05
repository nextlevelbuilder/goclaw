package instagram

// instagramCreds holds encrypted credentials stored in channel_instances.credentials.
type instagramCreds struct {
	UserAccessToken string `json:"user_access_token"`
	AppSecret       string `json:"app_secret"`
	VerifyToken     string `json:"verify_token"`
}

// instagramInstanceConfig holds non-secret config from channel_instances.config JSONB.
type instagramInstanceConfig struct {
	InstagramUserID string `json:"instagram_user_id"` // IG_ID - Instagram Business Account ID
	Features        struct {
		AutoReply bool `json:"auto_reply"`
		Typing    bool `json:"typing"`
	} `json:"features"`
	SessionOptions struct {
		SessionTimeout string `json:"session_timeout"`
	} `json:"session_options"`
	AllowFrom []string `json:"allow_from,omitempty"`
}

// --- Webhook payloads ---

// WebhookPayload is the top-level Instagram webhook event payload.
type WebhookPayload struct {
	Object string         `json:"object"`
	Entry  []WebhookEntry `json:"entry"`
}

// WebhookEntry is one Instagram user's events within a webhook delivery.
type WebhookEntry struct {
	ID        string           `json:"id"` // instagram_user_id
	Time      int64            `json:"time"`
	Messaging []MessagingEvent `json:"messaging,omitempty"`
}

// MessagingEvent is a single Instagram messaging event.
// Meta webhook wire shape for Instagram Messaging uses the same `sender`/`recipient`
// keys as Messenger. Do NOT rename these tags — doing so silently breaks inbound parsing.
type MessagingEvent struct {
	Sender    Sender           `json:"sender"`
	Recipient Recipient        `json:"recipient"`
	Timestamp int64            `json:"timestamp"`
	Message   *IncomingMessage `json:"message,omitempty"`
}

// Sender is a minimal Instagram sender reference.
type Sender struct {
	ID string `json:"id"`
}

// Recipient is a minimal Instagram recipient reference.
type Recipient struct {
	ID string `json:"id"`
}

// IncomingMessage holds an Instagram message.
type IncomingMessage struct {
	ID               string            `json:"mid"`
	Text             string            `json:"text,omitempty"`
	Attachments      []Attachment      `json:"attachments,omitempty"`
	Reactions        []Reaction        `json:"reactions,omitempty"`
	IsMessageEcho    bool              `json:"is_echo,omitempty"`
	ReplyTo          *ReplyTo          `json:"reply_to,omitempty"`
	AppreciationInfo *AppreciationInfo `json:"appreciation_info,omitempty"`
}

// Attachment is an Instagram media attachment.
type Attachment struct {
	Type    string            `json:"type"` // "image", "video", "audio", "file"
	MediaID string            `json:"media_id"`
	URL     string            `json:"url"`
	Payload AttachmentPayload `json:"payload"`
}

// AttachmentPayload holds attachment metadata.
type AttachmentPayload struct {
	URL string `json:"url"`
}

// Reaction represents an emoji reaction.
type Reaction struct {
	Emoji string `json:"emoji"`
}

// ReplyTo contains the message being replied to.
type ReplyTo struct {
	ID string `json:"mid"`
}

// AppreciationInfo contains appreciation data for messages.
type AppreciationInfo struct {
	HasAppreciation bool `json:"has_appreciation"`
}
