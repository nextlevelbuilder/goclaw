// Package facebook implements the Facebook Fanpage channel for GoClaw.
// Supports: comment auto-reply, Messenger inbox auto-reply, first inbox DM.
package facebook

// facebookCreds holds encrypted credentials stored in channel_instances.credentials.
type facebookCreds struct {
	PageAccessToken string `json:"page_access_token"`
	AppSecret       string `json:"app_secret"`
	VerifyToken     string `json:"verify_token"`
}

// facebookInstanceConfig holds non-secret config from channel_instances.config JSONB.
type facebookInstanceConfig struct {
	PageID   string   `json:"page_id"`
	Features struct {
		CommentReply       bool `json:"comment_reply"`
		FirstInbox         bool `json:"first_inbox"`
		MessengerAutoReply bool `json:"messenger_auto_reply"`
	} `json:"features"`
	CommentReplyOptions struct {
		IncludePostContext bool `json:"include_post_context"`
		MaxThreadDepth     int  `json:"max_thread_depth"`
	} `json:"comment_reply_options"`
	MessengerOptions struct {
		SessionTimeout string `json:"session_timeout"`
	} `json:"messenger_options"`
	PostContextCacheTTL string   `json:"post_context_cache_ttl"`
	// FirstInboxMessage is the DM text sent to commenters (first-inbox feature).
	// Defaults to Vietnamese if empty. Operators should set this to match their page language.
	FirstInboxMessage string   `json:"first_inbox_message,omitempty"`
	AllowFrom         []string `json:"allow_from,omitempty"`
}

// --- Webhook payloads ---

// WebhookPayload is the top-level Facebook webhook event payload.
type WebhookPayload struct {
	Object string         `json:"object"`
	Entry  []WebhookEntry `json:"entry"`
}

// WebhookEntry is one page's events within a webhook delivery.
type WebhookEntry struct {
	ID        string           `json:"id"`   // page_id
	Time      int64            `json:"time"`
	Changes   []WebhookChange  `json:"changes,omitempty"`   // feed events (comments, posts)
	Messaging []MessagingEvent `json:"messaging,omitempty"` // Messenger events
}

// WebhookChange is a single change event for feed subscriptions.
type WebhookChange struct {
	Field string      `json:"field"` // "feed", "mention", etc.
	Value ChangeValue `json:"value"`
}

// ChangeValue holds the details of a feed change event.
type ChangeValue struct {
	From        FBUser `json:"from"`
	Item        string `json:"item"`      // "comment", "post", "status"
	CommentID   string `json:"comment_id"`
	PostID      string `json:"post_id"`
	ParentID    string `json:"parent_id"` // parent comment ID for nested replies
	Message     string `json:"message"`
	Verb        string `json:"verb"`        // "add", "edit", "remove"
	CreatedTime int64  `json:"created_time"`
}

// MessagingEvent is a single Messenger inbox event.
type MessagingEvent struct {
	Sender    FBUser           `json:"sender"`
	Recipient FBUser           `json:"recipient"`
	Timestamp int64            `json:"timestamp"`
	Message   *IncomingMessage `json:"message,omitempty"`
	Postback  *Postback        `json:"postback,omitempty"`
}

// IncomingMessage holds a Messenger text/attachment message.
type IncomingMessage struct {
	MID         string       `json:"mid"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Postback holds a Messenger postback event.
type Postback struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

// Attachment is a Messenger media attachment.
type Attachment struct {
	Type    string            `json:"type"` // "image", "video", "audio", "file"
	Payload AttachmentPayload `json:"payload"`
}

// AttachmentPayload holds the URL of a media attachment.
type AttachmentPayload struct {
	URL string `json:"url"`
}

// FBUser is a minimal Facebook user reference.
type FBUser struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// --- Graph API response types ---

// GraphComment is a Facebook comment object from the Graph API.
type GraphComment struct {
	ID          string `json:"id"`
	Message     string `json:"message"`
	From        FBUser `json:"from"`
	CreatedTime string `json:"created_time"`
}

// GraphPost is a Facebook post object from the Graph API.
type GraphPost struct {
	ID          string `json:"id"`
	Message     string `json:"message"`
	Story       string `json:"story,omitempty"`
	CreatedTime string `json:"created_time"`
}

// GraphPaging holds cursor-based pagination for Graph API list responses.
type GraphPaging struct {
	Cursors struct {
		Before string `json:"before"`
		After  string `json:"after"`
	} `json:"cursors"`
	Next string `json:"next,omitempty"`
}

// GraphListResponse is a generic Graph API list response.
type GraphListResponse[T any] struct {
	Data   []T         `json:"data"`
	Paging GraphPaging `json:"paging,omitempty"`
}

// graphErrorBody is the error envelope returned by Graph API on failures.
type graphErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
		Subcode int    `json:"error_subcode"`
	} `json:"error"`
}
