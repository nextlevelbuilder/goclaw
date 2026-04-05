// Package teams implements the Microsoft Teams channel via Azure Bot Framework REST API.
// Uses webhook-based message delivery (no Bot Framework SDK).
// Reference: internal/channels/feishu/ (closest pattern — both webhook-based).
package teams

// Activity represents a Bot Framework Activity object.
// See: https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-api-reference
type Activity struct {
	Type           string           `json:"type"`
	ID             string           `json:"id"`
	Timestamp      string           `json:"timestamp"`
	ServiceURL     string           `json:"serviceUrl"`
	ChannelID      string           `json:"channelId"`
	From           ChannelAccount   `json:"from"`
	Conversation   ConversationID   `json:"conversation"`
	Recipient      ChannelAccount   `json:"recipient"`
	Text           string           `json:"text"`
	TextFormat     string           `json:"textFormat,omitempty"`
	ReplyToID      string           `json:"replyToId,omitempty"`
	Value          any              `json:"value,omitempty"`
	MembersAdded   []ChannelAccount `json:"membersAdded,omitempty"`
	MembersRemoved []ChannelAccount `json:"membersRemoved,omitempty"`
}

// ChannelAccount identifies a user or bot in a conversation.
type ChannelAccount struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ConversationID identifies a conversation.
type ConversationID struct {
	ID               string `json:"id"`
	ConversationType string `json:"conversationType,omitempty"` // "personal", "groupChat", or "channel"
}

// tokenResponse is the Azure AD OAuth2 token response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	TokenType   string `json:"token_type"`
}

// openIDConfig is the OpenID Connect metadata response.
type openIDConfig struct {
	JWKSURI string `json:"jwks_uri"`
	Issuer  string `json:"issuer"`
}
