// Package pancake implements the Pancake (pages.fm) channel for GoClaw.
// Pancake acts as a unified proxy for Facebook, Zalo OA, Instagram, TikTok, WhatsApp, Line.
// A single Pancake API key gives access to all connected platforms — no per-platform OAuth needed.
package pancake

import "encoding/json"

// pancakeCreds holds encrypted credentials stored in channel_instances.credentials.
type pancakeCreds struct {
	APIKey          string `json:"api_key"`                  // User-level Pancake API key
	PageAccessToken string `json:"page_access_token"`        // Page-level token for all page APIs
	WebhookSecret   string `json:"webhook_secret,omitempty"` // Optional HMAC-SHA256 verification
}

// pancakeInstanceConfig holds non-secret config from channel_instances.config JSONB.
type pancakeInstanceConfig struct {
	PageID        string `json:"page_id"`
	WebhookPageID string `json:"webhook_page_id,omitempty"` // native platform page ID sent in webhooks (e.g. Facebook page ID vs Pancake internal ID)
	Platform      string `json:"platform,omitempty"` // set explicitly via UI; auto-detected at Start() as fallback for existing channels
	// Known values: facebook/instagram/threads/tiktok/youtube/shopee/line/google/chat_plugin/lazada/tokopedia
	// Excluded (have native channel implementations): telegram/zalo/whatsapp
	Features struct {
		InboxReply   bool `json:"inbox_reply"`
		CommentReply bool `json:"comment_reply"`
		FirstInbox   bool `json:"first_inbox"`  // send one-time DM to commenter after comment reply
		AutoReact    bool `json:"auto_react"`   // auto-like user comments on Facebook (platform=facebook only)
	} `json:"features"`
	CommentReplyOptions struct {
		IncludePostContext bool     `json:"include_post_context"` // prepend post text to comment content
		Filter             string   `json:"filter"`               // "all" | "keyword" (default: all)
		Keywords           []string `json:"keywords"`             // required when filter = "keyword"
	} `json:"comment_reply_options"`
	AutoReactOptions    *AutoReactOptions `json:"auto_react_options,omitempty"`
	FirstInboxMessage   string            `json:"first_inbox_message,omitempty"`    // custom DM text; defaults to built-in message
	PostContextCacheTTL string            `json:"post_context_cache_ttl,omitempty"` // e.g. "30m"; defaults to 15m
	AllowFrom           []string          `json:"allow_from,omitempty"`
	BlockReply          *bool             `json:"block_reply,omitempty"` // override gateway block_reply (nil = inherit)
}

// AutoReactOptions holds per-page scope filters for Facebook auto-react.
// Nil = no scope filter (react all). Deny lists override allow lists.
type AutoReactOptions struct {
	AllowPostIDs []string `json:"allow_post_ids,omitempty"`
	DenyPostIDs  []string `json:"deny_post_ids,omitempty"`
	AllowUserIDs []string `json:"allow_user_ids,omitempty"`
	DenyUserIDs  []string `json:"deny_user_ids,omitempty"`
}

// --- Webhook payload types ---
// These types match the actual Pancake (pages.fm) webhook delivery format.
// Top-level envelope has optional "event_type" + nested "data" containing
// "conversation" and "message" objects.

// WebhookEvent is the top-level Pancake webhook delivery envelope.
type WebhookEvent struct {
	EventType string          `json:"event_type,omitempty"` // "messaging", may be empty
	PageID    string          `json:"page_id,omitempty"`    // top-level page_id (some formats)
	Data      json.RawMessage `json:"data"`
}

// WebhookData is the "data" envelope inside a Pancake webhook event.
type WebhookData struct {
	Conversation WebhookConversation `json:"conversation"`
	Message      WebhookMessage      `json:"message"`
	PageID       string              `json:"page_id,omitempty"` // page_id may appear here or top-level
}

// WebhookConversation holds the conversation metadata from a Pancake webhook.
type WebhookConversation struct {
	ID          string        `json:"id"`                     // format: "pageID_senderID"
	Type        string        `json:"type"`                   // "INBOX" or "COMMENT"
	PostID      string        `json:"post_id,omitempty"`      // present for COMMENT events
	AssigneeIDs []string      `json:"assignee_ids,omitempty"` // Pancake staff IDs assigned to this conversation
	From        WebhookSender `json:"from"`
	Snippet     string        `json:"snippet,omitempty"`
}

// WebhookSender identifies the message sender.
type WebhookSender struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email,omitempty"`
	PageCustomerID string `json:"page_customer_id,omitempty"`
}

// WebhookMessage holds the message payload from a Pancake webhook.
type WebhookMessage struct {
	ID              string              `json:"id"`
	Message         string              `json:"message,omitempty"`          // primary text content
	OriginalMessage string              `json:"original_message,omitempty"` // unformatted fallback
	Content         string              `json:"content,omitempty"`          // legacy field
	From            *WebhookSender      `json:"from,omitempty"`             // actual sender of this message
	Attachments     []MessageAttachment `json:"attachments,omitempty"`
	CreatedAt       json.Number         `json:"created_at,omitempty"`
}

// MessageAttachment represents a media attachment in a Pancake webhook message.
type MessageAttachment struct {
	Type string `json:"type"` // "image", "video", "file"
	URL  string `json:"url"`
}

// MessagingData is the normalized internal representation used after parsing.
type MessagingData struct {
	PageID         string
	ConversationID string
	PostID         string // present for COMMENT events; empty for INBOX
	Type           string // "INBOX" or "COMMENT"
	Platform       string // platform identifier from Pancake: facebook/instagram/tiktok/line/etc. See pancakeInstanceConfig.Platform for full list.
	AssigneeIDs    []string
	Message        MessagingMessage
}

// MessagingMessage is the normalized message used by the handler.
type MessagingMessage struct {
	ID          string
	Content     string
	SenderID    string
	SenderName  string
	Attachments []MessageAttachment
}

// --- API response types ---

// PageInfo holds page metadata from GET /pages response.
type PageInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"` // facebook/zalo/instagram/tiktok/whatsapp/line
	Avatar   string `json:"avatar,omitempty"`
}

// SendMessageRequest is the POST body for sending a message via Pancake API.
type SendMessageRequest struct {
	Action     string   `json:"action"`
	Message    string   `json:"message,omitempty"`
	MessageID  string   `json:"message_id,omitempty"`  // required for reply_comment: ID of the comment being replied to
	ContentIDs []string `json:"content_ids,omitempty"`
}

// PancakePost represents a page post from Pancake GET /pages/{id}/posts.
type PancakePost struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at,omitempty"`
}

// UploadResponse is returned by POST /pages/{id}/upload_contents.
type UploadResponse struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
}

// apiError wraps a Pancake API error response.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return e.Message
}
