package mattermost

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Attachment represents a rich message attachment for Mattermost.
// It can be specified in OutboundMessage.Metadata["attachments"] as JSON.
type Attachment struct {
	Fallback   string            `json:"fallback,omitempty"`
	Color      string            `json:"color,omitempty"`
	Pretext    string            `json:"pretext,omitempty"`
	AuthorName string            `json:"author_name,omitempty"`
	AuthorLink string            `json:"author_link,omitempty"`
	AuthorIcon string            `json:"author_icon,omitempty"`
	Title      string            `json:"title,omitempty"`
	TitleLink  string            `json:"title_link,omitempty"`
	Text       string            `json:"text,omitempty"`
	Fields     []AttachmentField `json:"fields,omitempty"`
	ImageURL   string            `json:"image_url,omitempty"`
	ThumbURL   string            `json:"thumb_url,omitempty"`
	Footer     string            `json:"footer,omitempty"`
	FooterIcon string            `json:"footer_icon,omitempty"`
}

// AttachmentField is a key-value field displayed in a message attachment.
type AttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// buildAttachments converts OutboundMessage metadata into Mattermost MessageAttachments.
// Returns nil if no attachment data is present.
//
// Supported metadata keys:
//   - "attachments": JSON array of Attachment objects
//   - "attachment_color": shorthand for a single attachment wrapping the message content
//   - "attachment_title": title for the shorthand attachment
func buildAttachments(msg bus.OutboundMessage) []*MMMessageAttachment {
	// Full attachments from metadata JSON
	if raw := msg.Metadata["attachments"]; raw != "" {
		var atts []Attachment
		if err := json.Unmarshal([]byte(raw), &atts); err != nil {
			slog.Debug("mattermost: invalid attachments JSON in metadata", "error", err)
		} else if len(atts) > 0 {
			return toMessageAttachments(atts)
		}
	}

	// Shorthand: wrap message content in a colored attachment
	color := msg.Metadata["attachment_color"]
	title := msg.Metadata["attachment_title"]
	if color != "" || title != "" {
		att := &MMMessageAttachment{
			Color:    color,
			Title:    title,
			Text:     msg.Content,
			Fallback: msg.Content,
		}
		return []*MMMessageAttachment{att}
	}

	return nil
}

// extractInlineAttachments parses ```attachment code blocks from message content
// and returns the remaining text + any parsed attachments.
//
// Format:
//
//	```attachment
//	{"color":"#FF8000","title":"Status","text":"All systems operational","fields":[...]}
//	```
func extractInlineAttachments(content string) (string, []*MMMessageAttachment) {
	const openTag = "```attachment"
	const closeTag = "```"

	var attachments []*MMMessageAttachment
	var remaining strings.Builder
	text := content

	for {
		start := strings.Index(text, openTag)
		if start == -1 {
			remaining.WriteString(text)
			break
		}

		remaining.WriteString(text[:start])
		after := text[start+len(openTag):]

		// Find closing ```
		end := strings.Index(after, closeTag)
		if end == -1 {
			// No closing tag — keep as-is
			remaining.WriteString(text[start:])
			break
		}

		jsonBlock := strings.TrimSpace(after[:end])
		text = after[end+len(closeTag):]

		// Try parsing as single attachment or array
		var att Attachment
		if err := json.Unmarshal([]byte(jsonBlock), &att); err == nil {
			attachments = append(attachments, toMessageAttachment(att))
			continue
		}

		var atts []Attachment
		if err := json.Unmarshal([]byte(jsonBlock), &atts); err == nil {
			attachments = append(attachments, toMessageAttachments(atts)...)
			continue
		}

		slog.Debug("mattermost: could not parse inline attachment block", "block", jsonBlock)
		remaining.WriteString(openTag + "\n" + jsonBlock + "\n" + closeTag)
	}

	return strings.TrimSpace(remaining.String()), attachments
}

func toMessageAttachment(a Attachment) *MMMessageAttachment {
	sa := &MMMessageAttachment{
		Fallback:   a.Fallback,
		Color:      a.Color,
		Pretext:    a.Pretext,
		AuthorName: a.AuthorName,
		AuthorLink: a.AuthorLink,
		AuthorIcon: a.AuthorIcon,
		Title:      a.Title,
		TitleLink:  a.TitleLink,
		Text:       a.Text,
		ImageURL:   a.ImageURL,
		ThumbURL:   a.ThumbURL,
		Footer:     a.Footer,
		FooterIcon: a.FooterIcon,
	}
	if sa.Fallback == "" && sa.Text != "" {
		sa.Fallback = sa.Text
	}
	for _, f := range a.Fields {
		sa.Fields = append(sa.Fields, &MMMessageAttachmentField{
			Title: f.Title,
			Value: f.Value,
			Short: f.Short,
		})
	}
	return sa
}

func toMessageAttachments(atts []Attachment) []*MMMessageAttachment {
	result := make([]*MMMessageAttachment, 0, len(atts))
	for _, a := range atts {
		result = append(result, toMessageAttachment(a))
	}
	return result
}
