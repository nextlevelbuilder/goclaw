package bitrix24

import (
	"context"
)

// sendChunkV2Public posts a single chunk via imbot.v2.Chat.Message.send.
// This is the modern (v2) chat-bot API used for PUBLIC replies in Open
// Channel sessions — Bitrix24 forwards them to the configured external
// connector (Zalo, FB, etc.). If replyToMID > 0, fields.replyId links the
// bot's reply to the inbound message in the Bitrix UI ("↩ tin gốc").
//
// Params shape verified live against tamgiac.bitrix24.com:
//
//	{
//	  "botId":    1058,
//	  "dialogId": "chat4878",
//	  "fields": {
//	    "message": "<chunk>",
//	    "replyId": 297178            // optional, only when > 0
//	  }
//	}
//
// See: https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/messages/chat-message-send.html
//
// Rate-limit retry semantics are delegated to callWithRateLimitRetry so
// they stay identical to the v1 whisper path.
func (c *Channel) sendChunkV2Public(ctx context.Context, chatID, chunk string, replyToMID int) error {
	botID := c.BotID()

	fields := map[string]any{
		"message": chunk,
	}
	// replyId is integer in v2 schema. Only set when caller passed a
	// valid MessageID — Atoi check already happened in resolveSendOptions.
	if replyToMID > 0 {
		fields["replyId"] = replyToMID
	}

	params := map[string]any{
		"botId":    botID,
		"dialogId": chatID,
		"fields":   fields,
	}

	return c.callWithRateLimitRetry(ctx, "imbot.v2.Chat.Message.send", params, chatID, botID)
}
