package max

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// defaultFormat is the value sent in NewMessageBody.format for outbound text.
// "markdown" lets Max parse common syntax (**bold**, _italic_, `code`, [link](url))
// and convert to its native markup. Empty string disables formatting.
const defaultFormat = "markdown"

// send is the production implementation of Channel.Send. Wired from max.go:Send().
//
// Flow:
//  1. Validate ChatID — must be a non-empty numeric string (DM thread or group).
//  2. Chunk content by 4000-char limit.
//  3. For each chunk: POST /messages with format=markdown.
//  4. After successful send, persist message_id of the LAST chunk so streaming
//     edits (Day 4) can target it.
//  5. Media attachments are skipped in Day 3 with a debug log; Day 4 implements
//     the upload flow.
//
// Error handling:
//   - Validation errors return immediately without retry (caller logic bug).
//   - Network/API errors propagate up; goclaw outbound dispatcher decides retry.
//   - On partial success (chunk N/M sent, M+1 failed), we return the error.
//     Goclaw will likely retry the whole message — we accept double-delivery
//     of early chunks as the lesser evil. Day 4 may add idempotency tokens.
func (c *Channel) send(ctx context.Context, msg bus.OutboundMessage) error {
	chatID, err := parseChatID(msg.ChatID)
	if err != nil {
		return fmt.Errorf("max send: %w", err)
	}

	chunks := chunkText(msg.Content)
	if len(chunks) == 0 && len(msg.Media) == 0 {
		// Nothing to send — agent produced empty content and no media.
		// This can happen with abort/cancel mid-generation; treat as no-op.
		return nil
	}

	// Day 3: log + skip media; Day 4 implements upload.
	if len(msg.Media) > 0 {
		slog.Debug("max: outbound media not yet implemented (Day 4)",
			"channel", c.Name(), "chat_id", msg.ChatID, "count", len(msg.Media))
	}

	// If chunks empty but media present, we still need to send the media.
	// Day 3 stub: skip silently. Day 4 will upload + send.
	if len(chunks) == 0 {
		return nil
	}

	var lastMessageID string
	for i, chunk := range chunks {
		req := SendMessageParams{
			ChatID: chatID,
			Body: SendMessageRequest{
				Text:   chunk,
				Format: defaultFormat,
			},
		}

		resp, err := c.client.SendMessage(ctx, req)
		if err != nil {
			slog.Warn("max: send chunk failed",
				"channel", c.Name(),
				"chat_id", msg.ChatID,
				"chunk", i+1, "of", len(chunks),
				"error", err,
			)
			return fmt.Errorf("send chunk %d/%d: %w", i+1, len(chunks), err)
		}
		lastMessageID = resp.MessageID

		slog.Debug("max: chunk sent",
			"channel", c.Name(),
			"chat_id", msg.ChatID,
			"chunk", i+1, "of", len(chunks),
			"message_id", resp.MessageID,
			"bytes", len(chunk),
		)
	}

	// Persist message_id for streaming edits (Day 4 will use this).
	// Key on chat_id — newest message_id wins. This matches Telegram's
	// `placeholders sync.Map` pattern and works because streaming only
	// edits the most recent placeholder.
	if lastMessageID != "" {
		c.placeholders.Store(msg.ChatID, lastMessageID)
		atomic.AddInt64(&c.sentCount, 1)
	}

	return nil
}

// parseChatID parses the goclaw bus.OutboundMessage.ChatID (string) into an int64
// suitable for the Max API ?chat_id= query parameter.
//
// goclaw bus uses string ChatIDs; for Max, we stored the numeric chat_id (DM
// thread or group ID) as the canonical chat identifier in inbound.go. So this
// is a simple strconv parse.
func parseChatID(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("ChatID is empty")
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ChatID %q is not a valid int64: %w", s, err)
	}
	if id == 0 {
		return 0, errors.New("ChatID is zero")
	}
	return id, nil
}

// lastMessageIDFor returns the last message_id sent into the given chat,
// or empty string if no message has been sent there yet. Used by streaming
// (Day 4) to find the placeholder to edit.
func (c *Channel) lastMessageIDFor(chatID string) string {
	v, ok := c.placeholders.Load(chatID)
	if !ok {
		return ""
	}
	id, _ := v.(string)
	return id
}
