package line

import (
	"context"
	"log/slog"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Send delivers an outbound message to a LINE chat.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	chatID := msg.ChatID
	text := formatForLINE(msg.Content)

	if text == "" && len(msg.Media) == 0 {
		return nil
	}

	// Send text messages.
	if text != "" {
		chunks := splitMessage(text, maxTextLength)
		if err := c.sendChunks(chatID, chunks); err != nil {
			return err
		}
	}

	// Send media attachments.
	for _, m := range msg.Media {
		imgMsg := linebot.NewImageMessage(m.URL, m.URL)
		if _, err := c.bot.PushMessage(chatID, imgMsg).Do(); err != nil {
			slog.Error("LINE: failed to push media", "chatID", chatID, "err", err)
		}
	}

	return nil
}

// sendChunks sends text chunks, using reply token if available and fresh.
//
// Returns nil silently when c.bot is unset — this happens in unit tests
// that exercise the higher-level scan/cleanup helpers without spinning up
// a real linebot.Client. Production Channel.New always sets bot.
func (c *Channel) sendChunks(chatID string, chunks []string) error {
	if c.bot == nil {
		return nil
	}
	// Try reply token first.
	if entry, ok := c.replyTokens.LoadAndDelete(chatID); ok {
		e := entry.(replyTokenEntry)
		if time.Since(e.receivedAt).Seconds() < float64(replyTokenTTL) {
			// Build up to maxReplyMessages.
			var msgs []linebot.SendingMessage
			for i, chunk := range chunks {
				if i >= maxReplyMessages {
					break
				}
				msgs = append(msgs, linebot.NewTextMessage(chunk))
			}
			if _, err := c.bot.ReplyMessage(e.token, msgs...).Do(); err != nil {
				slog.Warn("LINE: reply failed, falling back to push", "err", err)
			} else {
				// If we sent all chunks via reply, done.
				if len(chunks) <= maxReplyMessages {
					return nil
				}
				// Push remaining chunks.
				chunks = chunks[maxReplyMessages:]
			}
		}
	}

	// Push remaining chunks one by one.
	for _, chunk := range chunks {
		if _, err := c.bot.PushMessage(chatID, linebot.NewTextMessage(chunk)).Do(); err != nil {
			slog.Error("LINE: push failed", "chatID", chatID, "err", err)
			return err
		}
	}
	return nil
}

// pushFlex unconditionally pushes a Flex Message bubble to the chat. Used by
// the draft watcher (no reply token available) and as a fallback when the
// reply token has expired.
func (c *Channel) pushFlex(chatID, altText string, flexJSON []byte) error {
	container, err := linebot.UnmarshalFlexMessageJSON(flexJSON)
	if err != nil {
		return err
	}
	if _, err := c.bot.PushMessage(chatID, linebot.NewFlexMessage(altText, container)).Do(); err != nil {
		slog.Error("LINE: pushFlex failed", "chatID", chatID, "err", err)
		return err
	}
	return nil
}

// replyFlex tries the cached reply token first (cheaper, no quota cost) and
// falls back to a push when the token is missing or expired. Used by the
// postback handler chain — every PostbackEvent has a fresh reply token.
func (c *Channel) replyFlex(chatID, altText string, flexJSON []byte) error {
	container, err := linebot.UnmarshalFlexMessageJSON(flexJSON)
	if err != nil {
		return err
	}
	msg := linebot.NewFlexMessage(altText, container)

	if entry, ok := c.replyTokens.LoadAndDelete(chatID); ok {
		e := entry.(replyTokenEntry)
		if time.Since(e.receivedAt).Seconds() < float64(replyTokenTTL) {
			if _, err := c.bot.ReplyMessage(e.token, msg).Do(); err == nil {
				return nil
			}
			slog.Warn("LINE: replyFlex token failed, falling back to push")
		}
	}
	if _, err := c.bot.PushMessage(chatID, msg).Do(); err != nil {
		slog.Error("LINE: replyFlex push fallback failed", "chatID", chatID, "err", err)
		return err
	}
	return nil
}
