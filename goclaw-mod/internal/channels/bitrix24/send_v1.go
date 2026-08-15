package bitrix24

import (
	"context"
)

// sendChunkV1Whisper posts a single chunk via imbot.message.add (v1) with
// SKIP_CONNECTOR=Y. This is the legacy chat-bot API; v1 and v2 run in
// parallel on every portal (per the official migration doc), so a bot
// registered through imbot.register can still invoke imbot.message.add
// without re-registration.
//
// Why v1 here instead of v2: imbot.v2.Chat.Message.send (used by the
// public path) does NOT expose any equivalent of SKIP_CONNECTOR. v1 is
// the only documented way to send a message that stays inside the Open
// Channel session and is NOT forwarded to the external connector
// (Zalo, FB, etc.) — i.e. the "whisper / internal-only" use case.
//
// Params shape (flat UPPER_SNAKE_CASE per v1 convention):
//
//	{
//	  "BOT_ID":         1058,
//	  "DIALOG_ID":      "chat4878",
//	  "MESSAGE":        "<chunk>",
//	  "SKIP_CONNECTOR": "Y"
//	}
//
// See: https://apidocs.bitrix24.com/api-reference/chat-bots/messages/imbot-message-add.html
//
// Limitations vs v2:
//   - No replyId equivalent; whisper replies don't render a "↩ tin gốc"
//     link. Acceptable trade-off since whisper visibility is staff-only.
//
// Rate-limit retry semantics are delegated to callWithRateLimitRetry so
// they stay identical to the v2 public path.
func (c *Channel) sendChunkV1Whisper(ctx context.Context, chatID, chunk string) error {
	botID := c.BotID()

	params := map[string]any{
		"BOT_ID":         botID,
		"DIALOG_ID":      chatID,
		"MESSAGE":        chunk,
		"SKIP_CONNECTOR": "Y",
	}

	return c.callWithRateLimitRetry(ctx, "imbot.message.add", params, chatID, botID)
}
