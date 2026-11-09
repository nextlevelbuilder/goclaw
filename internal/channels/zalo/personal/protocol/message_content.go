package protocol

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
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
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Href        string  `json:"href"`
	Thumb       string  `json:"thumb"`
	ThumbURL    string  `json:"thumbUrl"`
	OriURL      string  `json:"oriUrl"`
	NormalURL   string  `json:"normalUrl"`
	Type        string  `json:"type"`
	ID          string  `json:"id"`
	StickerID   string  `json:"stickerId"`
	Duration    float64 `json:"duration"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Desc        string  `json:"desc"`
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

// URL returns the best available attachment URL across Zalo content variants.
func (a *Attachment) URL() string {
	return a.URLFor("")
}

// URLFor applies the msgType-specific priority. Image/video payloads often put
// an HTML page in href and the actual media in oriUrl; link payloads put the
// target page in href. Using one order for all types either downloads HTML or
// misses the link target.
func (a *Attachment) URLFor(msgType string) string {
	if a == nil {
		return ""
	}
	var candidates []string
	switch msgType {
	case "share.link", "chat.link":
		candidates = []string{a.Href, a.OriURL, a.NormalURL, a.Thumb, a.ThumbURL}
	case "chat.photo", "chat.video", "chat.video.msg", "chat.gif", "chat.doodle":
		candidates = []string{a.OriURL, a.NormalURL, a.Href, a.Thumb, a.ThumbURL}
	default:
		candidates = []string{a.Href, a.OriURL, a.NormalURL, a.Thumb, a.ThumbURL}
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

// imageExts lists file extensions recognized as images by the agent's vision pipeline.
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// IsImage reports whether the attachment href points to an image file.
// Checks both file extension and Zalo CDN path patterns (e.g. /jpg/, /png/).
func (a *Attachment) IsImage() bool {
	if a == nil {
		return false
	}
	for _, candidate := range []string{a.OriURL, a.NormalURL, a.Href, a.Thumb, a.ThumbURL} {
		if isImageURL(candidate) {
			return true
		}
	}
	return false
}

func isImageURL(rawURL string) bool {
	if strings.TrimSpace(rawURL) == "" {
		return false
	}
	path := strings.SplitN(rawURL, "?", 2)[0]
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
	if att.URL() != "" {
		if att.Title != "" {
			return fmt.Sprintf("[User sent a file: %s]", att.Title)
		}
		return "[User sent a file]"
	}
	return "[User sent a non-text message]"
}
