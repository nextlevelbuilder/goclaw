package mattermost

// Lightweight Mattermost types — replaces the heavy mattermost/server/public/model SDK.
// Only the fields actually used by GoClaw are included.

// MMPost represents a Mattermost post (message).
type MMPost struct {
	Id        string   `json:"id,omitempty"`
	ChannelId string   `json:"channel_id"`
	UserId    string   `json:"user_id,omitempty"`
	RootId    string   `json:"root_id,omitempty"`
	Message   string   `json:"message"`
	Type      string   `json:"type,omitempty"`
	FileIds   []string `json:"file_ids,omitempty"`

	// Props is used for rich message attachments
	Props map[string]any `json:"props,omitempty"`
}

// MMPostPatch represents a partial update to a post.
type MMPostPatch struct {
	Message *string `json:"message,omitempty"`
}

// MMUser represents a Mattermost user.
type MMUser struct {
	Id        string `json:"id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// DisplayName returns a human-readable name, preferring nickname then full name then username.
func (u *MMUser) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	full := u.FirstName
	if u.LastName != "" {
		if full != "" {
			full += " "
		}
		full += u.LastName
	}
	if full != "" {
		return full
	}
	return u.Username
}

// MMTeam represents a Mattermost team.
type MMTeam struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// MMChannel represents a Mattermost channel.
type MMChannel struct {
	Id   string `json:"id"`
	Type string `json:"type"` // "O" (open), "P" (private), "D" (direct), "G" (group)
}

// MMReaction represents a Mattermost emoji reaction.
type MMReaction struct {
	UserId    string `json:"user_id"`
	PostId    string `json:"post_id"`
	EmojiName string `json:"emoji_name"`
}

// MMFileInfo represents metadata about an uploaded file.
type MMFileInfo struct {
	Id       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// MMFileUploadResponse is the response from file upload.
type MMFileUploadResponse struct {
	FileInfos []*MMFileInfo `json:"file_infos"`
}

// MMWebSocketEvent represents a WebSocket event from Mattermost.
type MMWebSocketEvent struct {
	Event     string         `json:"event"`
	Data      map[string]any `json:"data"`
	Broadcast map[string]any `json:"broadcast,omitempty"`
	Seq       int64          `json:"seq"`
}

// MMMessageAttachment represents a Slack-compatible rich message attachment.
type MMMessageAttachment struct {
	Fallback   string                       `json:"fallback,omitempty"`
	Color      string                       `json:"color,omitempty"`
	Pretext    string                       `json:"pretext,omitempty"`
	AuthorName string                       `json:"author_name,omitempty"`
	AuthorLink string                       `json:"author_link,omitempty"`
	AuthorIcon string                       `json:"author_icon,omitempty"`
	Title      string                       `json:"title,omitempty"`
	TitleLink  string                       `json:"title_link,omitempty"`
	Text       string                       `json:"text,omitempty"`
	Fields     []*MMMessageAttachmentField  `json:"fields,omitempty"`
	ImageURL   string                       `json:"image_url,omitempty"`
	ThumbURL   string                       `json:"thumb_url,omitempty"`
	Footer     string                       `json:"footer,omitempty"`
	FooterIcon string                       `json:"footer_icon,omitempty"`
}

// MMMessageAttachmentField is a key-value field in a message attachment.
type MMMessageAttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// Channel type constants matching Mattermost API values.
const (
	ChannelTypeDirect  = "D"
	ChannelTypeGroup   = "G"
	ChannelTypeOpen    = "O"
	ChannelTypePrivate = "P"
)

// WebSocket event type constants.
const (
	WebsocketEventPosted     = "posted"
	WebsocketEventPostEdited = "post_edited"
)

// setAttachments sets rich message attachments on a post via Props.
func (p *MMPost) setAttachments(atts []*MMMessageAttachment) {
	if len(atts) == 0 {
		return
	}
	if p.Props == nil {
		p.Props = make(map[string]any)
	}
	p.Props["attachments"] = atts
}
