package telegram

import (
	"strings"

	"github.com/mymmrac/telego"
)

// detectMention checks if a Telegram message mentions the bot.
// Checks both msg.Text/Entities (text messages) and msg.Caption/CaptionEntities (photo/media messages).
func (c *Channel) detectMention(msg *telego.Message, botUsername string) bool {
	if botUsername == "" {
		return false
	}
	lowerBot := strings.ToLower(botUsername)

	// Check both text entities and caption entities (photos use Caption, not Text).
	for _, pair := range []struct {
		entities []telego.MessageEntity
		text     string
	}{
		{msg.Entities, msg.Text},
		{msg.CaptionEntities, msg.Caption},
	} {
		if pair.text == "" {
			continue
		}
		for _, entity := range pair.entities {
			if entity.Type == "mention" {
				mentioned := pair.text[entity.Offset : entity.Offset+entity.Length]
				if strings.EqualFold(mentioned, "@"+botUsername) {
					return true
				}
			}
			if entity.Type == "bot_command" {
				cmdText := pair.text[entity.Offset : entity.Offset+entity.Length]
				if strings.Contains(strings.ToLower(cmdText), "@"+lowerBot) {
					return true
				}
			}
		}
	}

	// Fallback: substring check in both text and caption
	if msg.Text != "" && strings.Contains(strings.ToLower(msg.Text), "@"+lowerBot) {
		return true
	}
	if msg.Caption != "" && strings.Contains(strings.ToLower(msg.Caption), "@"+lowerBot) {
		return true
	}

	// Reply to bot's message = implicit mention
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
		if msg.ReplyToMessage.From.Username == botUsername {
			return true
		}
	}

	return false
}

// hasOtherMention checks if the message mentions any user/bot other than ours.
// Used by "yield" mention mode: if another entity is explicitly @mentioned, this bot yields.
// Checks both "mention" entities (@username) and "bot_command" entities (/cmd@bot).
func (c *Channel) hasOtherMention(msg *telego.Message, myUsername string) bool {
	lowerMy := strings.ToLower(myUsername)

	for _, pair := range []struct {
		entities []telego.MessageEntity
		text     string
	}{
		{msg.Entities, msg.Text},
		{msg.CaptionEntities, msg.Caption},
	} {
		if pair.text == "" {
			continue
		}
		for _, entity := range pair.entities {
			if entity.Type == "mention" {
				mentioned := pair.text[entity.Offset : entity.Offset+entity.Length]
				mentionedLower := strings.ToLower(mentioned)
				// Skip our own mention
				if mentionedLower == "@"+lowerMy {
					continue
				}
				// Any other @mention in the message → another bot/user was called
				return true
			}
			if entity.Type == "bot_command" {
				cmdText := pair.text[entity.Offset : entity.Offset+entity.Length]
				cmdLower := strings.ToLower(cmdText)
				// Commands addressed to another bot: /cmd@other_bot
				if atIdx := strings.Index(cmdLower, "@"); atIdx > 0 {
					target := cmdLower[atIdx+1:]
					if target != lowerMy {
						return true
					}
				}
			}
		}
	}
	return false
}

// isServiceMessage returns true if the Telegram message is a service/system message
// (member added/removed, title changed, pinned, etc.) rather than a user-sent message.
// Service messages have no text, caption, or media content.
func isServiceMessage(msg *telego.Message) bool {
	// Has text or caption → user message
	if msg.Text != "" || msg.Caption != "" {
		return false
	}

	// Has media → user message (photo, audio, video, document, sticker, etc.)
	if msg.Photo != nil || msg.Audio != nil || msg.Video != nil ||
		msg.Document != nil || msg.Voice != nil || msg.VideoNote != nil ||
		msg.Sticker != nil || msg.Animation != nil || msg.Contact != nil ||
		msg.Location != nil || msg.Venue != nil || msg.Poll != nil {
		return false
	}

	// No user content — likely a service message (new_chat_members, left_chat_member,
	// new_chat_title, new_chat_photo, pinned_message, etc.)
	return true
}
