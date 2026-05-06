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

	// Streaming handoff: if FinalizeStream stored a placeholder messageID
	// for this chat, edit it with the first chunk instead of sending a
	// fresh message. This replaces the streaming preview with the final
	// markdown-formatted answer, avoiding a duplicate.
	//
	// We consume the placeholder (LoadAndDelete) so subsequent Send calls
	// in the same chat (e.g. error notifications) don't accidentally edit
	// the now-finalized message.
	placeholderID := c.consumePlaceholder(msg.ChatID)

	// Panic safety: if anything between here and the end of send panics,
	// restore the consumed placeholder so subsequent Send attempts can
	// still find and edit it. A re-panic preserves the original failure
	// for the caller / runtime to handle. We deliberately do NOT swallow
	// the panic — that would mask bugs.
	defer func() {
		if r := recover(); r != nil {
			if placeholderID != "" {
				c.placeholders.Store(msg.ChatID, placeholderID)
				slog.Warn("max: panic in send — placeholder restored",
					"channel", c.Name(), "chat_id", msg.ChatID,
					"placeholder", placeholderID, "panic", r)
			} else {
				slog.Warn("max: panic in send",
					"channel", c.Name(), "chat_id", msg.ChatID, "panic", r)
			}
			panic(r) // re-raise
		}
	}()

	// If only media: send a single message with the attachments and no text.
	if len(chunks) == 0 {
		if placeholderID != "" {
			// Edit the placeholder to remove "💭 Thinking..." then attach media
			// in a follow-up. Max EditMessage doesn't support attachments,
			// so we delete the placeholder and send fresh — accepting the
			// brief flicker as the lesser evil vs. orphaned placeholder.
			c.bestEffortDeletePlaceholder(ctx, placeholderID)
		}
		return c.sendOneChunk(ctx, msg.ChatID, chatID, "", attachments)
	}

	var lastMessageID string
	for i, chunk := range chunks {
		// Attach media only on the first chunk.
		var attsForThisChunk []Attachment
		if i == 0 {
			attsForThisChunk = attachments
		}

		// First chunk: prefer editing the streaming placeholder if one exists.
		// Subsequent chunks always go via SendMessage.
		var mid string
		if i == 0 && placeholderID != "" && len(attsForThisChunk) == 0 {
			// Edit path — placeholder exists and no media (Max EditMessage
			// doesn't support attachments; falls back to send if media present).
			mid, err = c.editPlaceholder(ctx, msg.ChatID, placeholderID, chunk)
			if err != nil {
				// Edit failed — fall back to a fresh send. Delete the stale
				// placeholder best-effort to keep the chat tidy.
				slog.Warn("max: edit placeholder failed, sending fresh message",
					"channel", c.Name(), "chat_id", msg.ChatID,
					"placeholder", placeholderID, "error", err)
				c.bestEffortDeletePlaceholder(ctx, placeholderID)
				mid, err = c.sendOneChunkAndReturnID(ctx, msg.ChatID, chatID, chunk, attsForThisChunk, i+1, len(chunks))
				if err != nil {
					return err
				}
			}
		} else {
			if i == 0 && placeholderID != "" && len(attsForThisChunk) > 0 {
				// Media-with-text on first chunk: can't edit (Max API limitation).
				// Delete placeholder, send fresh.
				c.bestEffortDeletePlaceholder(ctx, placeholderID)
			}
			mid, err = c.sendOneChunkAndReturnID(ctx, msg.ChatID, chatID, chunk, attsForThisChunk, i+1, len(chunks))
			if err != nil {
				return err
			}
		}
		lastMessageID = mid
	}

	// Persist message_id of the LAST chunk so a future streaming run can
	// know which message it edited. We only store when at least one chunk
	// was sent via fresh POST (i.e. lastMessageID came from SendMessage,
	// not from an edit of a consumed placeholder). After editing a
	// finalized placeholder, the mid points to the user-visible answer —
	// re-storing would cause the next Send call to overwrite that answer
	// instead of sending a new message.
	if lastMessageID != "" && placeholderID == "" {
		c.placeholders.Store(msg.ChatID, lastMessageID)
		atomic.AddInt64(&c.sentCount, 1)
	} else if lastMessageID != "" {
		// Even when we edited a placeholder, still bump the counter for
		// metrics consistency. Don't store the mid — see comment above.
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
// or empty string if no message has been sent there yet.
func (c *Channel) lastMessageIDFor(chatID string) string {
	v, ok := c.placeholders.Load(chatID)
	if !ok {
		return ""
	}
	id, _ := v.(string)
	return id
}

// consumePlaceholder atomically loads and removes the placeholder messageID
// for the given chat. Used by Send() to detect that a streaming preview is
// in flight and should be edited rather than replaced by a new message.
//
// Returns "" if no placeholder is registered.
//
// We delete on read so that subsequent Send calls within the same chat
// (e.g. error notifications, follow-up messages) don't accidentally edit a
// message that has already been finalized.
func (c *Channel) consumePlaceholder(chatID string) string {
	v, ok := c.placeholders.LoadAndDelete(chatID)
	if !ok {
		return ""
	}
	id, _ := v.(string)
	return id
}

// editPlaceholder edits the placeholder message with the final formatted
// chunk text. Uses Max's PUT /messages with `format: "markdown"` so the
// final response renders with proper formatting (bold, italic, code).
//
// Returns the message_id from the edit response (typically equal to the
// input mid; we trust the API).
func (c *Channel) editPlaceholder(
	ctx context.Context,
	chatIDStr string,
	placeholderID string,
	text string,
) (string, error) {
	resp, err := c.client.EditMessage(ctx, EditMessageParams{
		MessageID: placeholderID,
		Body: EditMessageRequest{
			Text:   text,
			Format: defaultFormat,
		},
	})
	if err != nil {
		return "", fmt.Errorf("edit placeholder %s: %w", placeholderID, err)
	}
	slog.Debug("max: placeholder edited with final response",
		"channel", c.Name(),
		"chat_id", chatIDStr,
		"message_id", placeholderID,
		"text_bytes", len(text),
	)
	// The API returns the same mid in resp.MessageID; fall back to the
	// input if for any reason the response shape is empty.
	if resp.MessageID != "" {
		return resp.MessageID, nil
	}
	return placeholderID, nil
}

// bestEffortDeletePlaceholder issues a DELETE for a placeholder we can't
// edit (e.g. when the Send carries media that EditMessage can't combine).
// Failure is logged but never propagates — leaving an orphaned placeholder
// is annoying but not catastrophic.
//
// Used as a recovery path; not on the happy edit path.
func (c *Channel) bestEffortDeletePlaceholder(ctx context.Context, placeholderID string) {
	if placeholderID == "" {
		return
	}
	if err := c.client.DeleteMessage(ctx, placeholderID); err != nil {
		slog.Debug("max: placeholder delete failed (non-fatal)",
			"channel", c.Name(), "message_id", placeholderID, "error", err)
	}
}
