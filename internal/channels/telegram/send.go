package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
)

// Error patterns for graceful handling (matching TS error constants in send.ts).
var (
	parseErrRe           = regexp.MustCompile(`(?i)can't parse entities|parse entities|find end of the entity`)
	messageNotModifiedRe = regexp.MustCompile(`(?i)message is not modified`)
	threadNotFoundRe     = regexp.MustCompile(`(?i)message thread not found`)
	messageTooLongRe     = regexp.MustCompile(`(?i)message is too long|entities too long`)
	htmlTagRe            = regexp.MustCompile(`<[^>]*>`)
)

const (
	sendMaxRetries     = 3
	sendRetryDelay     = 2 * time.Second
	maxSplitDepth      = 5               // max recursion depth for "message too long" splitting
	photoSizeThreshold = 5 * 1024 * 1024 // 5 MB — images larger than this are sent as documents to avoid Telegram compression
)

// stripHTML removes HTML tags and unescapes HTML entities for plain-text fallback.
func stripHTML(s string) string {
	return html.UnescapeString(htmlTagRe.ReplaceAllString(s, ""))
}

// isRetryableNetworkErr checks if a Telegram API error is a transient network/server error
// worth retrying. Covers transport-level failures and Telegram 5xx server errors.
func isRetryableNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	// Check for Telegram API 5xx errors via typed error.
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) && apiErr.ErrorCode >= 500 {
		return true
	}
	// Transport-level errors (timeout, reset, DNS, etc.)
	s := err.Error()
	return strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "lookup") // DNS resolution failure
}

// isPostConnectNetworkErr checks if the error likely occurred AFTER reaching the server
// (timeout, connection reset, EOF) vs before (DNS lookup failure, connection refused).
// Used to decide if a request may have landed despite the error.
func isPostConnectNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	// Telegram 5xx means the server received the request.
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) && apiErr.ErrorCode >= 500 {
		return true
	}
	s := err.Error()
	return (strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "EOF")) && !strings.Contains(s, "lookup")
}

// retrySend wraps a Telegram send call with retry logic for transient network errors.
// Parse errors are NOT retried (handled by caller's HTML fallback).
// resetFn is called before each retry (e.g. to seek file handles back to start). Can be nil.
func (c *Channel) retrySend(ctx context.Context, name string, resetFn func(), fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, sendOverallTimeout)
	defer cancel()

	var err error
	for attempt := 1; attempt <= sendMaxRetries; attempt++ {
		err = fn(ctx)
		if err == nil {
			return nil
		}
		// Don't retry parse errors — caller handles HTML fallback
		if parseErrRe.MatchString(err.Error()) {
			return err
		}
		if !isRetryableNetworkErr(err) || attempt == sendMaxRetries {
			return err
		}

		// If we hit a network-level connectivity issue (likely IPv6 routing),
		// arm sticky IPv4 fallback. Only triggers on "unreachable" — not timeouts
		// (which can be rate-limiting) or DNS errors (unrelated to IPv6).
		if strings.Contains(err.Error(), "unreachable") {
			c.enableIPv4Only()
		}

		slog.Warn("telegram send retry",
			"func", name, "attempt", attempt, "max", sendMaxRetries, "error", err)
		if resetFn != nil {
			resetFn()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sendRetryDelay * time.Duration(attempt)):
		}
	}
	return err
}

// Send delivers an outbound message to a Telegram chat.
// Supports text-only messages and messages with media attachments.
// Reads metadata for reply-to-message and forum thread routing.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("telegram bot not running")
	}

	// Use localKey for sync.Map lookups (composite key with topic suffix).
	localKey := msg.ChatID
	if lk := msg.Metadata["local_key"]; lk != "" {
		localKey = lk
	}

	// Parse raw Telegram chat ID (strips :topic:N suffix).
	chatID, err := parseRawChatID(localKey)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Parse reply/thread IDs from metadata.
	var replyToMsgID, threadID int
	if v := msg.Metadata["reply_to_message_id"]; v != "" {
		fmt.Sscanf(v, "%d", &replyToMsgID)
	}
	if v := msg.Metadata["message_thread_id"]; v != "" {
		fmt.Sscanf(v, "%d", &threadID)
	}

	// Fallback: extract threadID from localKey suffix (e.g. "-100123:topic:42").
	// This covers cases where metadata is absent, such as pairing approval notifications
	// routed via SendToChannel which only has a chatID string.
	if threadID == 0 {
		if idx := strings.Index(localKey, ":topic:"); idx > 0 {
			fmt.Sscanf(localKey[idx+7:], "%d", &threadID)
		} else if idx := strings.Index(localKey, ":thread:"); idx > 0 {
			fmt.Sscanf(localKey[idx+8:], "%d", &threadID)
		}
	}

	// Placeholder update (e.g. LLM retry notification): edit the placeholder
	// but keep it alive for the final response. Don't stop typing or cleanup.
	if msg.Metadata["placeholder_update"] == "true" {
		if pID, ok := c.placeholders.Load(localKey); ok {
			_ = c.editMessage(ctx, chatID, pID.(int), msg.Content)
		}
		return nil
	}

	// Stop thinking animation
	if stop, ok := c.stopThinking.Load(localKey); ok {
		if cf, ok := stop.(*thinkingCancel); ok {
			cf.Cancel()
		}
		c.stopThinking.Delete(localKey)
	}

	// Stop typing indicator controller (TTL keepalive)
	if ctrl, ok := c.typingCtrls.LoadAndDelete(localKey); ok {
		ctrl.(*typing.Controller).Stop()
	}

	// NO_REPLY cleanup: content is empty when agent suppresses reply (prompt injection, etc.).
	// Clean up placeholder, then return without sending any message.
	if msg.Content == "" && len(msg.Media) == 0 {
		if pID, ok := c.placeholders.Load(localKey); ok {
			c.placeholders.Delete(localKey)
			_ = c.deleteMessage(ctx, chatID, pID.(int))
		}
		return nil
	}

	// Handle media attachments if present
	if len(msg.Media) > 0 {
		// Delete placeholder since we're sending media
		if pID, ok := c.placeholders.Load(localKey); ok {
			c.placeholders.Delete(localKey)
			_ = c.deleteMessage(ctx, chatID, pID.(int))
		}
		return c.sendMediaMessage(ctx, chatID, msg, replyToMsgID, threadID)
	}

	// Text-only message
	htmlContent := markdownToTelegramHTML(msg.Content)
	chunks := chunkHTML(htmlContent, telegramMaxMessageLen)

	// If a stream message exists (stored by FinalizeStream), edit the first chunk
	// into it instead of deleting. This prevents the message from vanishing
	// when HTML conversion makes content exceed the size limit.
	startChunk := 0
	if pID, ok := c.placeholders.Load(localKey); ok {
		c.placeholders.Delete(localKey)
		msgID := pID.(int)

		if msgID == -1 {
			// SIGNAL from stream: A message transport send likely landed but ID was never retrieved.
			// Swallow the first chunk ONLY if there are more chunks to come (minimizes visible duplicate).
			// If it is the ONLY chunk, we deliver it anyway to guarantee the user sees the answer.
			if len(chunks) > 1 {
				slog.Warn("telegram: ghost message detected, skipping first chunk of multi-chunk response", "chat_id", chatID)
				startChunk = 1
			}
		} else {
			err := c.editMessage(ctx, chatID, msgID, chunks[0])
			if err == nil {
				startChunk = 1 // first chunk edited into stream message
			} else if isPostConnectNetworkErr(err) && len(chunks) > 1 {
				// Mid-stream timeout/lost connection: the edit likely reached Telegram
				// but the response was lost. Swallow and skip chunk 0 ONLY for multi-chunk
				// messages where the rest of the answer is still coming.
				slog.Warn("telegram: final edit timed out or lost, skipping chunk 0 of multi-chunk response",
					"chat_id", chatID, "message_id", msgID, "error", err)
				startChunk = 1
			} else {
				// Edit failed definitely (400 rejection), or a single-chunk edit timed out.
				// For single-chunk answers, we delete (best-effort) and send fresh to
				// guarantee the user gets the content.
				_ = c.deleteMessage(ctx, chatID, msgID)
			}
		}
	}

	// Send remaining chunks (or all chunks if no stream message was edited).
	for i := startChunk; i < len(chunks); i++ {
		replyTo := 0
		if i == 0 {
			replyTo = replyToMsgID // only first chunk replies to user's message
		}
		if err := c.sendHTML(ctx, chatID, chunks[i], replyTo, threadID); err != nil {
			return err
		}
	}
	return nil
}

// sendMediaMessage sends a message with media attachments.
// Ref: TS src/telegram/send.ts → sendMessageTelegram with mediaUrl
func (c *Channel) sendMediaMessage(ctx context.Context, chatID int64, msg bus.OutboundMessage, replyTo, threadID int) error {
	chatIDObj := tu.ID(chatID)

	for _, media := range msg.Media {
		// Determine caption (use message content for first media, or media caption)
		caption := media.Caption
		if caption == "" && msg.Content != "" {
			caption = msg.Content
			msg.Content = "" // only use for first media
		}

		// Convert caption from markdown to Telegram HTML (same as regular messages).
		// If the HTML caption exceeds Telegram's 1024-byte limit, skip caption entirely
		// and send the full text as a separate message. Truncating HTML at a byte boundary
		// can split tags (e.g. cut inside <code>...</code>) causing parse errors.
		var followUpText string
		if caption != "" {
			caption = markdownToTelegramHTML(caption)
			if len(caption) > telegramCaptionMaxLen {
				followUpText = caption
				caption = ""
			}
		}

		// Honor MediaMaxBytes for outbound sends.
		// Prevents attempting to upload huge files that would fail via Telegram Bot API or local proxy.
		maxBytes := c.config.MediaMaxBytes
		if maxBytes == 0 {
			if c.config.APIServer != "" {
				maxBytes = localAPIDefaultMaxBytes
			} else {
				maxBytes = defaultMediaMaxBytes
			}
		}
		if info, err := os.Stat(media.URL); err == nil && info.Size() > maxBytes {
			return fmt.Errorf("outbound media too large: %d bytes (limit %d)", info.Size(), maxBytes)
		}

		// Send based on content type.
		// Large images (>photoSizeThreshold) are sent as documents to avoid Telegram compression.
		ct := strings.ToLower(media.ContentType)
		switch {
		case strings.HasPrefix(ct, "image/"):
			sendAsDoc := false
			if info, statErr := os.Stat(media.URL); statErr == nil && info.Size() > photoSizeThreshold {
				sendAsDoc = true
				slog.Info("large image, sending as document to preserve quality", "path", media.URL, "size", info.Size())
			}
			if sendAsDoc {
				if err := c.sendDocument(ctx, chatIDObj, media.URL, caption, replyTo, threadID); err != nil {
					return err
				}
			} else if err := c.sendPhoto(ctx, chatIDObj, media.URL, caption, replyTo, threadID); err != nil {
				return err
			}
		case strings.HasPrefix(ct, "video/"):
			if err := c.sendVideo(ctx, chatIDObj, media.URL, caption, replyTo, threadID); err != nil {
				return err
			}
		case strings.HasPrefix(ct, "audio/"):
			if err := c.sendAudio(ctx, chatIDObj, media.URL, caption, replyTo, threadID); err != nil {
				return err
			}
		default:
			if err := c.sendDocument(ctx, chatIDObj, media.URL, caption, replyTo, threadID); err != nil {
				return err
			}
		}
		// Only reply to the first media item
		replyTo = 0

		// Send follow-up text if caption was split.
		// followUpText is already HTML (from markdownToTelegramHTML above), so
		// just chunk and send — do NOT convert again (double-escaping breaks entities).
		if followUpText != "" {
			chunks := chunkHTML(followUpText, telegramMaxMessageLen)
			for _, chunk := range chunks {
				if err := c.sendHTML(ctx, chatID, chunk, 0, threadID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// sendHTML sends a single HTML message, falling back to plain text if Telegram rejects the HTML.
// replyTo and threadID are optional (0 = omit). General topic (1) is handled by resolveThreadIDForSend.
func (c *Channel) sendHTML(ctx context.Context, chatID int64, htmlContent string, replyTo, threadID int) error {
	return c.sendHTMLWithDepth(ctx, chatID, htmlContent, replyTo, threadID, 0)
}

func (c *Channel) sendHTMLWithDepth(ctx context.Context, chatID int64, htmlContent string, replyTo, threadID, depth int) error {
	tgMsg := tu.Message(tu.ID(chatID), htmlContent)
	tgMsg.ParseMode = telego.ModeHTML

	// TS ref: buildTelegramThreadParams() — General topic (1) must be omitted.
	if sendThreadID := resolveThreadIDForSend(threadID); sendThreadID > 0 {
		tgMsg.MessageThreadID = sendThreadID
	}
	if replyTo > 0 {
		tgMsg.ReplyParameters = &telego.ReplyParameters{
			MessageID:                replyTo,
			AllowSendingWithoutReply: true, // don't fail if replied-to message was deleted
		}
	}

	err := c.retrySend(ctx, "sendMessage", nil, func(ctx context.Context) error {
		_, e := c.bot.SendMessage(ctx, tgMsg)
		return e
	})

	if err != nil {
		errStr := err.Error()

		// Case 1: Message too long. Split into smaller chunks and send individually.
		if messageTooLongRe.MatchString(errStr) {
			if depth >= maxSplitDepth {
				return fmt.Errorf("max split depth (%d) exceeded: %w", maxSplitDepth, err)
			}
			slog.Warn("Telegram rejected message as too long, splitting further", "len", len(htmlContent), "depth", depth)
			newMaxLen := len(htmlContent) / 2
			if newMaxLen < 100 {
				return err // too small to split meaningfully
			}

			innerChunks := chunkHTML(htmlContent, newMaxLen)
			for i, chunk := range innerChunks {
				r := 0
				if i == 0 {
					r = replyTo
				}
				if sendErr := c.sendHTMLWithDepth(ctx, chatID, chunk, r, threadID, depth+1); sendErr != nil {
					return sendErr
				}
			}
			return nil
		}

		// Case 2: Parse error. Fallback to plain text.
		if parseErrRe.MatchString(errStr) {
			slog.Warn("HTML parse failed, falling back to plain text", "error", err)
			tgMsg.ParseMode = ""
			tgMsg.Text = stripHTML(htmlContent)
			_, err = c.bot.SendMessage(ctx, tgMsg)

			// If plain text is STILL too long, split it.
			if err != nil && messageTooLongRe.MatchString(err.Error()) {
				slog.Warn("Plain text fallback too long, splitting further", "len", len(tgMsg.Text))
				innerChunks := chunkPlainText(tgMsg.Text, 4000) // use default safe limit
				for i, chunk := range innerChunks {
					msg := tu.Message(tu.ID(chatID), chunk)
					msg.ReplyParameters = tgMsg.ReplyParameters
					if i > 0 {
						msg.ReplyParameters = nil
					}
					msg.MessageThreadID = tgMsg.MessageThreadID
					_, err = c.bot.SendMessage(ctx, msg)
					if err != nil {
						return err
					}
				}
				return nil
			}
		}

		// Case 3: Thread not found. Re-check err (may have changed after Case 2 fallback).
		if err != nil && tgMsg.MessageThreadID != 0 && threadNotFoundRe.MatchString(err.Error()) {
			slog.Warn("thread not found, retrying without message_thread_id", "thread_id", tgMsg.MessageThreadID)
			tgMsg.MessageThreadID = 0
			_, err = c.bot.SendMessage(ctx, tgMsg)
		}
	}
	return err
}

// sendPhoto sends a photo message.
func (c *Channel) sendPhoto(ctx context.Context, chatID telego.ChatID, filePath, caption string, replyTo, threadID int) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open photo %s: %w", filePath, err)
	}
	defer file.Close()

	params := &telego.SendPhotoParams{
		ChatID:  chatID,
		Photo:   telego.InputFile{File: file},
		Caption: caption,
	}
	if caption != "" {
		params.ParseMode = telego.ModeHTML
	}
	if sendThreadID := resolveThreadIDForSend(threadID); sendThreadID > 0 {
		params.MessageThreadID = sendThreadID
	}
	if replyTo > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}

	err = c.retrySend(ctx, "sendPhoto", func() { file.Seek(0, 0) }, func(ctx context.Context) error {
		_, e := c.bot.SendPhoto(ctx, params)
		return e
	})
	if err != nil && parseErrRe.MatchString(err.Error()) {
		slog.Warn("sendPhoto: HTML parse failed, retrying with plain text caption", "error", err)
		file.Seek(0, 0)
		params.ParseMode = ""
		params.Caption = stripHTML(params.Caption)
		_, err = c.bot.SendPhoto(ctx, params)
	}
	if err != nil && params.MessageThreadID != 0 && threadNotFoundRe.MatchString(err.Error()) {
		slog.Warn("sendPhoto: thread not found, retrying without thread", "thread_id", params.MessageThreadID)
		file.Seek(0, 0)
		params.MessageThreadID = 0
		_, err = c.bot.SendPhoto(ctx, params)
	}
	return err
}

// sendVideo sends a video message.
func (c *Channel) sendVideo(ctx context.Context, chatID telego.ChatID, filePath, caption string, replyTo, threadID int) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open video %s: %w", filePath, err)
	}
	defer file.Close()

	params := &telego.SendVideoParams{
		ChatID:  chatID,
		Video:   telego.InputFile{File: file},
		Caption: caption,
	}
	if caption != "" {
		params.ParseMode = telego.ModeHTML
	}
	if sendThreadID := resolveThreadIDForSend(threadID); sendThreadID > 0 {
		params.MessageThreadID = sendThreadID
	}
	if replyTo > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}

	err = c.retrySend(ctx, "sendVideo", func() { file.Seek(0, 0) }, func(ctx context.Context) error {
		_, e := c.bot.SendVideo(ctx, params)
		return e
	})
	if err != nil && parseErrRe.MatchString(err.Error()) {
		slog.Warn("sendVideo: HTML parse failed, retrying with plain text caption", "error", err)
		file.Seek(0, 0)
		params.ParseMode = ""
		params.Caption = stripHTML(params.Caption)
		_, err = c.bot.SendVideo(ctx, params)
	}
	if err != nil && params.MessageThreadID != 0 && threadNotFoundRe.MatchString(err.Error()) {
		slog.Warn("sendVideo: thread not found, retrying without thread", "thread_id", params.MessageThreadID)
		file.Seek(0, 0)
		params.MessageThreadID = 0
		_, err = c.bot.SendVideo(ctx, params)
	}
	return err
}

// sendAudio sends an audio message.
func (c *Channel) sendAudio(ctx context.Context, chatID telego.ChatID, filePath, caption string, replyTo, threadID int) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open audio %s: %w", filePath, err)
	}
	defer file.Close()

	params := &telego.SendAudioParams{
		ChatID:  chatID,
		Audio:   telego.InputFile{File: file},
		Caption: caption,
	}
	if caption != "" {
		params.ParseMode = telego.ModeHTML
	}
	if sendThreadID := resolveThreadIDForSend(threadID); sendThreadID > 0 {
		params.MessageThreadID = sendThreadID
	}
	if replyTo > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}

	err = c.retrySend(ctx, "sendAudio", func() { file.Seek(0, 0) }, func(ctx context.Context) error {
		_, e := c.bot.SendAudio(ctx, params)
		return e
	})
	if err != nil && parseErrRe.MatchString(err.Error()) {
		slog.Warn("sendAudio: HTML parse failed, retrying with plain text caption", "error", err)
		file.Seek(0, 0)
		params.ParseMode = ""
		params.Caption = stripHTML(params.Caption)
		_, err = c.bot.SendAudio(ctx, params)
	}
	if err != nil && params.MessageThreadID != 0 && threadNotFoundRe.MatchString(err.Error()) {
		slog.Warn("sendAudio: thread not found, retrying without thread", "thread_id", params.MessageThreadID)
		file.Seek(0, 0)
		params.MessageThreadID = 0
		_, err = c.bot.SendAudio(ctx, params)
	}
	return err
}

// sendDocument sends a document/file message.
func (c *Channel) sendDocument(ctx context.Context, chatID telego.ChatID, filePath, caption string, replyTo, threadID int) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open document %s: %w", filePath, err)
	}
	defer file.Close()

	params := &telego.SendDocumentParams{
		ChatID:   chatID,
		Document: telego.InputFile{File: file},
		Caption:  caption,
	}
	if caption != "" {
		params.ParseMode = telego.ModeHTML
	}
	if sendThreadID := resolveThreadIDForSend(threadID); sendThreadID > 0 {
		params.MessageThreadID = sendThreadID
	}
	if replyTo > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}

	err = c.retrySend(ctx, "sendDocument", func() { file.Seek(0, 0) }, func(ctx context.Context) error {
		_, e := c.bot.SendDocument(ctx, params)
		return e
	})
	if err != nil && parseErrRe.MatchString(err.Error()) {
		slog.Warn("sendDocument: HTML parse failed, retrying with plain text caption", "error", err)
		file.Seek(0, 0)
		params.ParseMode = ""
		params.Caption = stripHTML(params.Caption)
		_, err = c.bot.SendDocument(ctx, params)
	}
	if err != nil && params.MessageThreadID != 0 && threadNotFoundRe.MatchString(err.Error()) {
		slog.Warn("sendDocument: thread not found, retrying without thread", "thread_id", params.MessageThreadID)
		file.Seek(0, 0)
		params.MessageThreadID = 0
		_, err = c.bot.SendDocument(ctx, params)
	}
	return err
}

// editMessage edits an existing message's text.
// Uses retrySend since edits are idempotent and may fail on transient network issues.
func (c *Channel) editMessage(ctx context.Context, chatID int64, messageID int, htmlText string) error {
	editMsg := tu.EditMessageText(tu.ID(chatID), messageID, htmlText)
	editMsg.ParseMode = telego.ModeHTML

	return c.retrySend(ctx, "editMessage", nil, func(ctx context.Context) error {
		_, err := c.bot.EditMessageText(ctx, editMsg)
		if err != nil {
			// Ignore "message is not modified" errors (idempotent edit)
			if messageNotModifiedRe.MatchString(err.Error()) {
				return nil
			}
			return err
		}
		return nil
	})
}

// deleteMessage deletes a message from the chat.
func (c *Channel) deleteMessage(ctx context.Context, chatID int64, messageID int) error {
	return c.bot.DeleteMessage(ctx, &telego.DeleteMessageParams{
		ChatID:    tu.ID(chatID),
		MessageID: messageID,
	})
}
