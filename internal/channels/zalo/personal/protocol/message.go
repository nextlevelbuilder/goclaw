package protocol

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Message is the interface for incoming Zalo messages (DM or group).
type Message interface {
	Type() ThreadType
	ThreadID() string
	IsSelf() bool
}

// TMessage is the raw JSON message payload from Zalo WebSocket.
type TMessage struct {
	MsgID   string  `json:"msgId"`
	UIDFrom string  `json:"uidFrom"`
	IDTo    string  `json:"idTo"`
	DName   string  `json:"dName"`
	TS      string  `json:"ts"`
	Content Content `json:"content"`
	MsgType string  `json:"msgType"`
	CMD     int     `json:"cmd"`
	ST      int     `json:"st"`
	AT      int     `json:"at"`
}

// TGroupMessage extends TMessage with group-specific fields.
type TGroupMessage struct {
	TMessage
	Mentions []*TMention `json:"mentions,omitempty"`
}

// TMention represents an @mention in a group message.
type TMention struct {
	UID  string      `json:"uid"` // user ID or "-1" for @all
	Pos  int         `json:"pos"`
	Len  int         `json:"len"`
	Type MentionType `json:"type"` // 0=individual, 1=all
}

// MentionType distinguishes individual vs @all mentions.
type MentionType int

const (
	MentionEach   MentionType = 0
	MentionAll    MentionType = 1
	MentionAllUID             = "-1"
)

// Content is a union type: can be a plain string or an attachment object.
// String is set for text messages; Raw is set for non-text (images, stickers, files).
type Content struct {
	String *string
	Raw    json.RawMessage // non-nil when content is a JSON object (attachment)
}

func (c *Content) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.String = &s
		return nil
	}
	c.Raw = slices.Clone(data) // preserve raw attachment payload
	return nil
}

func (c Content) MarshalJSON() ([]byte, error) {
	if c.String != nil {
		return json.Marshal(c.String)
	}
	return []byte("null"), nil
}

// Text returns the plain text content, or empty string for non-text.
func (c Content) Text() string {
	if c.String != nil {
		return *c.String
	}
	return ""
}

// Attachment holds parsed fields from a non-text content object.
type Attachment struct {
	Title string `json:"title"`
	Href  string `json:"href"`
}

// ParseAttachment extracts attachment metadata from non-text content.
// Returns nil if content is plain text or unrecognized.
func (c Content) ParseAttachment() *Attachment {
	if c.Raw == nil {
		return nil
	}
	var att Attachment
	if json.Unmarshal(c.Raw, &att) != nil {
		return &Attachment{} // unrecognized but non-text
	}
	return &att
}

// imageExts lists file extensions recognized as images by the agent's vision pipeline.
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// IsImage reports whether the attachment href points to an image file.
// Checks both file extension and Zalo CDN path patterns (e.g. /jpg/, /png/).
func (a *Attachment) IsImage() bool {
	if a == nil || a.Href == "" {
		return false
	}
	path := strings.SplitN(a.Href, "?", 2)[0]
	if imageExts[strings.ToLower(filepath.Ext(path))] {
		return true
	}
	// Zalo CDN paths like https://f20-zpc.zdn.vn/jpg/...
	lower := strings.ToLower(path)
	return strings.Contains(lower, "/jpg/") || strings.Contains(lower, "/png/") ||
		strings.Contains(lower, "/gif/") || strings.Contains(lower, "/webp/")
}

// AttachmentText returns a human-readable placeholder for non-text content.
func (c Content) AttachmentText() string {
	att := c.ParseAttachment()
	if att == nil {
		return ""
	}
	if att.IsImage() {
		if att.Title != "" {
			return fmt.Sprintf("[User sent an image: %s]", att.Title)
		}
		return "[User sent an image]"
	}
	if att.Href != "" {
		if att.Title != "" {
			return fmt.Sprintf("[User sent a file: %s]", att.Title)
		}
		return "[User sent a file]"
	}
	return "[User sent a non-text message]"
}

// UserMessage represents a DM (type=0).
type UserMessage struct {
	Data     TMessage
	threadID string
	isSelf   bool
}

// NewUserMessage creates a UserMessage, resolving self-sent messages.
func NewUserMessage(selfUID string, data TMessage) UserMessage {
	msg := UserMessage{Data: data, threadID: data.UIDFrom}
	msg.isSelf = data.UIDFrom == DefaultUIDSelf

	if data.UIDFrom == DefaultUIDSelf {
		msg.threadID = data.IDTo
		msg.Data.UIDFrom = selfUID
	}
	if data.IDTo == DefaultUIDSelf {
		msg.Data.IDTo = selfUID
	}
	return msg
}

func (m UserMessage) Type() ThreadType { return ThreadTypeUser }
func (m UserMessage) ThreadID() string { return m.threadID }
func (m UserMessage) IsSelf() bool     { return m.isSelf }

// GroupMessage represents a group message (type=1).
type GroupMessage struct {
	Data     TGroupMessage
	threadID string
	isSelf   bool
}

// NewGroupMessage creates a GroupMessage, resolving self-sent messages.
func NewGroupMessage(selfUID string, data TGroupMessage) GroupMessage {
	g := GroupMessage{Data: data, threadID: data.IDTo}
	g.isSelf = data.UIDFrom == DefaultUIDSelf
	if data.UIDFrom == DefaultUIDSelf {
		g.Data.UIDFrom = selfUID
	}
	return g
}

func (m GroupMessage) Type() ThreadType { return ThreadTypeGroup }
func (m GroupMessage) ThreadID() string { return m.threadID }
func (m GroupMessage) IsSelf() bool     { return m.isSelf }
