package mattermost

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
)

// attachmentBlockRe matches a fenced ```attachment block holding a JSON
// object or array, on its own lines.
var attachmentBlockRe = regexp.MustCompile("(?ms)^```attachment[ \\t]*\\r?\\n(.*?)\\r?\\n```[ \\t]*$")

// mmAttachment is a Mattermost message attachment (the Slack-compatible card
// rendered under a post). Interactive actions are deliberately not
// supported: they need an integration endpoint to call back.
type mmAttachment struct {
	Fallback   string              `json:"fallback,omitempty"`
	Color      string              `json:"color,omitempty"`
	Pretext    string              `json:"pretext,omitempty"`
	AuthorName string              `json:"author_name,omitempty"`
	AuthorLink string              `json:"author_link,omitempty"`
	AuthorIcon string              `json:"author_icon,omitempty"`
	Title      string              `json:"title,omitempty"`
	TitleLink  string              `json:"title_link,omitempty"`
	Text       string              `json:"text,omitempty"`
	Fields     []mmAttachmentField `json:"fields,omitempty"`
	ImageURL   string              `json:"image_url,omitempty"`
	ThumbURL   string              `json:"thumb_url,omitempty"`
	Footer     string              `json:"footer,omitempty"`
	FooterIcon string              `json:"footer_icon,omitempty"`
}

type mmAttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// visible reports whether the attachment would render anything.
func (a *mmAttachment) visible() bool {
	return a.Title != "" || a.Text != "" || a.Pretext != "" || len(a.Fields) > 0 || a.ImageURL != ""
}

// extractAttachments removes valid ```attachment blocks from the text and
// returns them parsed. Blocks that are not valid attachment JSON stay in the
// text untouched, so nothing the model wrote is lost.
func extractAttachments(text string) (string, []mmAttachment) {
	if !strings.Contains(text, "```attachment") {
		return text, nil
	}

	var out []mmAttachment
	rest := attachmentBlockRe.ReplaceAllStringFunc(text, func(block string) string {
		parsed, ok := parseAttachmentJSON(attachmentBlockRe.FindStringSubmatch(block)[1])
		if !ok {
			return block
		}
		out = append(out, parsed...)
		return ""
	})
	if len(out) == 0 {
		return text, nil
	}
	// Extra blank lines left by a removed block render as a single paragraph
	// break in Markdown, so only the ends are trimmed.
	return strings.TrimSpace(rest), out
}

// parseAttachmentJSON accepts a single attachment object or an array.
func parseAttachmentJSON(raw string) ([]mmAttachment, bool) {
	raw = strings.TrimSpace(raw)
	var list []mmAttachment
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			slog.Debug("mattermost: invalid attachment block", "error", err)
			return nil, false
		}
	} else {
		var one mmAttachment
		if err := json.Unmarshal([]byte(raw), &one); err != nil {
			slog.Debug("mattermost: invalid attachment block", "error", err)
			return nil, false
		}
		list = []mmAttachment{one}
	}

	for i := range list {
		if !list[i].visible() {
			return nil, false
		}
		if list[i].Fallback == "" {
			list[i].Fallback = firstNonEmpty(list[i].Title, list[i].Text, list[i].Pretext)
		}
	}
	return list, len(list) > 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
