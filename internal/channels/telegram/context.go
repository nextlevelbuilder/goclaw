package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"
)

// MessageContext holds enriched context extracted from a Telegram message.
// Ref: TS src/telegram/bot-message-context.ts → buildTelegramMessageContext()
type MessageContext struct {
	ForwardInfo  *ForwardInfo
	ReplyInfo    *ReplyInfo
	LocationInfo *LocationInfo
}

// ForwardInfo contains metadata about a forwarded message.
// Ref: TS normalizeForwardedContext()
type ForwardInfo struct {
	From     string    // sender name or channel title
	FromType string    // "user", "channel", "supergroup", "hidden"
	Date     time.Time // original message date
}

// ReplyInfo contains metadata about the message being replied to.
// Ref: TS describeReplyTarget()
type ReplyInfo struct {
	Sender     string // sender name
	Body       string // quoted message text
	IsBotReply bool   // true if replying to bot's own message
}

// LocationInfo contains geographic coordinates.
// Ref: TS extractTelegramLocation()
type LocationInfo struct {
	Latitude  float64
	Longitude float64
}

// buildMessageContext extracts forward, reply, and location context from a Telegram message.
func buildMessageContext(msg *telego.Message, botUsername string) *MessageContext {
	ctx := &MessageContext{}

	ctx.ForwardInfo = extractForwardInfo(msg)
	ctx.ReplyInfo = extractReplyInfo(msg, botUsername)
	ctx.LocationInfo = extractLocationInfo(msg)

	return ctx
}

// enrichContentWithContext appends forward/reply/location context to the message content.
func enrichContentWithContext(content string, msgCtx *MessageContext) string {
	if msgCtx == nil {
		return content
	}

	var result strings.Builder

	// Prepend forward context
	if msgCtx.ForwardInfo != nil {
		dateStr := msgCtx.ForwardInfo.Date.Format("2006-01-02 15:04")
		result.WriteString(fmt.Sprintf("[Forwarded from %s at %s]\n", msgCtx.ForwardInfo.From, dateStr))
	}

	result.WriteString(content)

	// Append reply context
	if msgCtx.ReplyInfo != nil && msgCtx.ReplyInfo.Body != "" {
		result.WriteString(fmt.Sprintf("\n\n[Replying to %s]\n%s\n[/Replying]",
			msgCtx.ReplyInfo.Sender, msgCtx.ReplyInfo.Body))
	}

	// Append location
	if msgCtx.LocationInfo != nil {
		result.WriteString(fmt.Sprintf("\n\nCoordinates: %.6f, %.6f",
			msgCtx.LocationInfo.Latitude, msgCtx.LocationInfo.Longitude))
	}

	return result.String()
}

// extractForwardInfo extracts forwarded message metadata.
// Ref: TS normalizeForwardedContext()
func extractForwardInfo(msg *telego.Message) *ForwardInfo {
	if msg.ForwardOrigin == nil {
		return nil
	}

	info := &ForwardInfo{}

	switch origin := msg.ForwardOrigin.(type) {
	case *telego.MessageOriginUser:
		user := origin.SenderUser
		info.From = buildUserName(&user)
		info.FromType = "user"
		info.Date = time.Unix(origin.Date, 0)
	case *telego.MessageOriginChat:
		info.From = origin.SenderChat.Title
		info.FromType = string(origin.SenderChat.Type)
		info.Date = time.Unix(origin.Date, 0)
	case *telego.MessageOriginChannel:
		info.From = origin.Chat.Title
		info.FromType = "channel"
		info.Date = time.Unix(origin.Date, 0)
	case *telego.MessageOriginHiddenUser:
		info.From = origin.SenderUserName
		info.FromType = "hidden"
		info.Date = time.Unix(origin.Date, 0)
	default:
		return nil
	}

	return info
}

// extractReplyInfo extracts replied-to message metadata.
// Ref: TS describeReplyTarget()
func extractReplyInfo(msg *telego.Message, botUsername string) *ReplyInfo {
	reply := msg.ReplyToMessage
	if reply == nil {
		return nil
	}

	info := &ReplyInfo{}

	// Determine sender name
	if reply.From != nil {
		info.Sender = buildUserName(reply.From)
		info.IsBotReply = (reply.From.Username == botUsername)
	} else {
		info.Sender = "unknown"
	}

	// Extract reply body
	if reply.Text != "" {
		info.Body = reply.Text
	} else if reply.Caption != "" {
		info.Body = reply.Caption
	}

	// Truncate long reply bodies
	if len(info.Body) > 500 {
		info.Body = info.Body[:500] + "..."
	}

	// Hint for bot replies: the full response is already in session history,
	// so the LLM doesn't need the full text here — just enough to identify it.
	if info.IsBotReply && info.Body != "" {
		info.Body += "\n(This is your previous response — full content is in session history above)"
	}

	return info
}

// extractLocationInfo extracts location data from a message.
// Ref: TS extractTelegramLocation()
func extractLocationInfo(msg *telego.Message) *LocationInfo {
	if msg.Location == nil {
		return nil
	}

	return &LocationInfo{
		Latitude:  msg.Location.Latitude,
		Longitude: msg.Location.Longitude,
	}
}

// buildUserName formats a Telegram user's display name.
func buildUserName(user *telego.User) string {
	if user == nil {
		return "unknown"
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name
}
