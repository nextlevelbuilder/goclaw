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
//  2. Upload media attachments (if any) via two-step Max upload flow.
//     Failed uploads are logged and dropped; we still send the text.
//  3. Chunk text content by 4000-char limit.
//  4. For each chunk: POST /messages with format=markdown.
//     Media is attached to the FIRST chunk only — sending it with every
//     chunk would re-upload the file or duplicate the attachment in chat.
//  5. After successful send, persist message_id of the LAST chunk so streaming
//     edits (Day 4.5) can target it.
//
// Error handling:
//   - ChatID validation errors return immediately without retry.
//   - Network/API errors propagate up; goclaw outbound dispatcher decides retry.
//   - On partial success (chunk N/M sent, M+1 failed), we return the error.
//     Goclaw will likely retry the whole message — we accept double-delivery
//     of early chunks as the lesser evil.
//   - Individual media upload failures are logged but do not fail the send;
//     the agent's text content is preserved.
func (c *Channel) send(ctx context.Context, msg bus.OutboundMessage) error {
	chatID, err := parseChatID(msg.ChatID)
	if err != nil {
		return fmt.Errorf("max send: %w", err)
	}

	chunks := chunkText(msg.Content)

	// Upload media first so we know what's actually attachable. Failed
	// uploads are logged and dropped from the attachment list — text
	// chunks still ship.
	attachments, _ := c.uploadAndAttachMedia(ctx, msg.Media)

	if len(chunks) == 0 && len(attachments) == 0 {
		// Nothing to send — agent produced empty content and no media
		// (or all media uploads failed). Caller's retry logic will decide
		// what to do; we return nil because there's no work for us.
		return nil
	}

	// If only media: send a single message with the attachments and no text.
	if len(chunks) == 0 {
		return c.sendOneChunk(ctx, msg.ChatID, chatID, "", attachments)
	}

	var lastMessageID string
	for i, chunk := range chunks {
		// Attach media only on the first chunk.
		var attsForThisChunk []Attachment
		if i == 0 {
			attsForThisChunk = attachments
		}

		mid, err := c.sendOneChunkAndReturnID(ctx, msg.ChatID, chatID, chunk, attsForThisChunk, i+1, len(chunks))
		if err != nil {
			return err
		}
		lastMessageID = mid
	}

	// Persist message_id for streaming edits (Day 4.5 will use this).
	// Key on chat_id — newest message_id wins. This matches Telegram's
	// `placeholders sync.Map` pattern and works because streaming only
	// edits the most recent placeholder.
	if lastMessageID != "" {
		c.placeholders.Store(msg.ChatID, lastMessageID)
		atomic.AddInt64(&c.sentCount, 1)
	}

	return nil
}

// sendOneChunkAndReturnID sends one chunk and returns the resulting message_id.
// Logs at debug level on success; logs and returns wrapped error on failure.
func (c *Channel) sendOneChunkAndReturnID(
	ctx context.Context,
	chatIDStr string, // for logs
	chatID int64,
	text string,
	attachments []Attachment,
	chunkIdx, chunkTotal int,
) (string, error) {
	req := SendMessageParams{
		ChatID: chatID,
		Body: SendMessageRequest{
			Text:        text,
			Format:      defaultFormat,
			Attachments: attachments,
		},
	}

	resp, err := c.client.SendMessage(ctx, req)
	if err != nil {
		slog.Warn("max: send chunk failed",
			"channel", c.Name(),
			"chat_id", chatIDStr,
			"chunk", chunkIdx, "of", chunkTotal,
			"attachments", len(attachments),
			"error", err,
		)
		return "", fmt.Errorf("send chunk %d/%d: %w", chunkIdx, chunkTotal, err)
	}

	slog.Debug("max: chunk sent",
		"channel", c.Name(),
		"chat_id", chatIDStr,
		"chunk", chunkIdx, "of", chunkTotal,
		"message_id", resp.MessageID,
		"text_bytes", len(text),
		"attachments", len(attachments),
	)
	return resp.MessageID, nil
}

// sendOneChunk is a convenience for the media-only case where chunkIdx/total
// are not interesting and we don't need the message_id (no chunking means no
// streaming continuation). Errors are wrapped consistently.
func (c *Channel) sendOneChunk(
	ctx context.Context,
	chatIDStr string,
	chatID int64,
	text string,
	attachments []Attachment,
) error {
	req := SendMessageParams{
		ChatID: chatID,
		Body: SendMessageRequest{
			Text:        text,
			Format:      defaultFormat,
			Attachments: attachments,
		},
	}
	resp, err := c.client.SendMessage(ctx, req)
	if err != nil {
		slog.Warn("max: send media-only failed",
			"channel", c.Name(), "chat_id", chatIDStr,
			"attachments", len(attachments), "error", err)
		return fmt.Errorf("send media-only: %w", err)
	}
	c.placeholders.Store(chatIDStr, resp.MessageID)
	atomic.AddInt64(&c.sentCount, 1)
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
