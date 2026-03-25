package googlechat

// SpaceEvent is the webhook event envelope from Google Chat.
type SpaceEvent struct {
	Type                     string     `json:"type"`            // "MESSAGE", "ADDED_TO_SPACE", "REMOVED_FROM_SPACE"
	EventTime                string     `json:"eventTime"`
	Message                  *GCMessage `json:"message,omitempty"`
	Space                    *GCSpace   `json:"space,omitempty"`
	User                     *GCUser    `json:"user,omitempty"`
	ConfigCompleteRedirectURL string    `json:"configCompleteRedirectUrl,omitempty"`
}

// GCMessage represents a Google Chat message.
type GCMessage struct {
	Name          string         `json:"name"`            // "spaces/SPACE_ID/messages/MSG_ID"
	Text          string         `json:"text"`            // Plain text (includes @mentions)
	ArgumentText  string         `json:"argumentText"`    // Text with bot mention stripped
	Sender        *GCUser        `json:"sender,omitempty"`
	CreateTime    string         `json:"createTime"`
	Thread        *GCThread      `json:"thread,omitempty"`
	Space         *GCSpace       `json:"space,omitempty"`
	Annotations   []GCAnnotation `json:"annotations,omitempty"`
	FormattedText string         `json:"formattedText,omitempty"`
}

// GCSpace represents a Google Chat space (room or DM).
type GCSpace struct {
	Name            string `json:"name"`        // "spaces/SPACE_ID"
	Type            string `json:"type"`        // "DM", "ROOM", "SPACE"
	DisplayName     string `json:"displayName"`
	SingleUserBotDm bool   `json:"singleUserBotDm,omitempty"`
}

// GetName returns the space name (nil-safe).
func (s *GCSpace) GetName() string {
	if s == nil {
		return ""
	}
	return s.Name
}

// GCUser represents a Google Chat user.
type GCUser struct {
	Name        string `json:"name"`        // "users/USER_ID"
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	Type        string `json:"type"`        // "HUMAN", "BOT"
}

// GetDisplayName returns the user display name (nil-safe).
func (u *GCUser) GetDisplayName() string {
	if u == nil {
		return ""
	}
	return u.DisplayName
}

// GetName returns the user resource name (nil-safe).
func (u *GCUser) GetName() string {
	if u == nil {
		return ""
	}
	return u.Name
}

// GCThread represents a message thread.
type GCThread struct {
	Name string `json:"name"` // "spaces/SPACE_ID/threads/THREAD_ID"
}

// GCAnnotation represents a message annotation (mentions, links, etc).
type GCAnnotation struct {
	Type        string         `json:"type"`             // "USER_MENTION", "SLASH_COMMAND"
	StartIndex  int            `json:"startIndex"`
	Length      int            `json:"length"`
	UserMention *GCUserMention `json:"userMention,omitempty"`
}

// GCUserMention represents a user mention annotation.
type GCUserMention struct {
	User *GCUser `json:"user"`
	Type string  `json:"type"` // "MENTION", "ALL"
}

// gcSendMessage is the request body for spaces.messages.create.
type gcSendMessage struct {
	Text string `json:"text"`
}

// gcReaction is the request body for adding a reaction.
type gcReaction struct {
	Emoji *gcEmoji `json:"emoji"`
}

// gcEmoji represents a Google Chat emoji.
type gcEmoji struct {
	Unicode string `json:"unicode"`
}
