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
	PageID   string `json:"page_id"`
	Platform string `json:"platform,omitempty"` // auto-detected at Start(): facebook/zalo/instagram/tiktok/whatsapp/line
	Features struct {
		InboxReply   bool `json:"inbox_reply"`
		CommentReply bool `json:"comment_reply"`
	} `json:"features"`
	AllowFrom  []string `json:"allow_from,omitempty"`
	BlockReply *bool    `json:"block_reply,omitempty"` // override gateway block_reply (nil = inherit)
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
	ID          string        `json:"id"`                    // format: "pageID_senderID"
	Type        string        `json:"type"`                  // "INBOX" or "COMMENT"
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
	Type           string // "INBOX" or "COMMENT"
	Platform       string // "facebook", "zalo", "instagram", "tiktok", "whatsapp", "line"
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
	ContentIDs []string `json:"content_ids,omitempty"`
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
