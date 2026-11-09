package personal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const maxTextLength = 2000

// sendRetryDelays is a variable so tests can avoid real backoff sleeps.
var sendRetryDelays = []time.Duration{time.Second, 3 * time.Second}

// Send delivers an outbound message to a Zalo chat.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	sess := c.session()
	if !c.IsRunning() || sess == nil {
		return fmt.Errorf("zalo_personal channel not running")
	}

	// Plain mode strips markdown; rich mode converts it to Zalo style spans.
	if !c.config.RichText {
		msg.Content = zalo.StripMarkdown(msg.Content)
	}

	// Stop typing indicator before sending response
	if ctrl, ok := c.typingCtrls.LoadAndDelete(msg.ChatID); ok {
		ctrl.(*typing.Controller).Stop()
	}

	threadType := protocol.ThreadTypeUser
	if c.IsGroupApproved(msg.ChatID) {
		threadType = protocol.ThreadTypeGroup
	} else if msg.Metadata != nil {
		if _, ok := msg.Metadata["group_id"]; ok {
			threadType = protocol.ThreadTypeGroup
			c.MarkGroupApproved(msg.ChatID)
		}
	}

	var quote *protocol.TMessage
	if c.config.QuoteReply && msg.Metadata != nil {
		if id := strings.TrimSpace(msg.Metadata["quote_msg_id"]); id != "" {
			if cached, ok := c.quoteCache.Get(id); ok {
				quote = &cached
			}
		}
	}

	// Send media attachments.
	for _, media := range msg.Media {
		if err := c.waitSend(ctx, msg.ChatID); err != nil {
			return err
		}

		var err error
		if protocol.IsImageFile(media.URL) {
			err = c.sendImage(ctx, sess, msg.ChatID, threadType, media.URL, media.Caption)
		} else {
			err = c.sendFile(ctx, sess, msg.ChatID, threadType, media.URL)
		}
		if err != nil {
			if c.handleSendError(err) {
				return err
			}
			slog.Warn("zalo_personal: failed to send media", "path", media.URL, "error", err)
		}
	}

	// Send text content (if any remains after media).
	if msg.Content != "" {
		return c.sendChunkedText(ctx, sess, msg.ChatID, threadType, msg.Content, quote)
	}
	return nil
}

func (c *Channel) waitSend(ctx context.Context, threadID string) error {
	if c.limiter == nil {
		return nil
	}
	return c.limiter.Wait(ctx, threadID)
}

// handleSendError reports terminal auth failures once and tells the caller
// whether the send must stop. Policy errors also stop; transient errors may be
// logged and skipped so the remaining content can still be delivered.
func (c *Channel) handleSendError(err error) bool {
	if err == nil {
		return false
	}
	if protocol.IsAuthError(err) {
		if c.authFailureReported.CompareAndSwap(false, true) {
			c.MarkFailed("Zalo session expired", err.Error(), channels.ChannelFailureKindAuth, false)
		}
		return true
	}
	return protocol.IsPolicyError(err)
}

// sendImage uploads and sends an image file to a Zalo thread.
func (c *Channel) sendImage(ctx context.Context, sess *protocol.Session, chatID string, threadType protocol.ThreadType, filePath, caption string) error {
	return retryTransient(ctx, func() error {
		upload, err := protocol.UploadImage(ctx, sess, chatID, threadType, filePath)
		if err != nil {
			return fmt.Errorf("upload: %w", err)
		}

		_, err = protocol.SendImage(ctx, sess, chatID, threadType, upload, caption)
		return err
	})
}

// sendFile uploads and sends a file to a Zalo thread.
func (c *Channel) sendFile(ctx context.Context, sess *protocol.Session, chatID string, threadType protocol.ThreadType, filePath string) error {
	ln := c.getListener()
	if ln == nil {
		return fmt.Errorf("listener not available for file upload")
	}
	return retryTransient(ctx, func() error {
		upload, err := protocol.UploadFile(ctx, sess, ln, chatID, threadType, filePath)
		if err != nil {
			return fmt.Errorf("upload: %w", err)
		}

		_, err = protocol.SendFile(ctx, sess, chatID, threadType, upload)
		return err
	})
}

func (c *Channel) sendChunkedText(ctx context.Context, sess *protocol.Session, chatID string, threadType protocol.ThreadType, text string, quote *protocol.TMessage) error {
	chunks := channels.ChunkMarkdown(text, maxTextLength)
	sentFirst := false
	for i, chunk := range chunks {
		opts := protocol.SendOptions{Text: chunk}
		if c.config.RichText {
			styled, spans := zalo.MarkdownToZaloStyles(chunk)
			opts.Text = styled
			for _, span := range spans {
				opts.Styles = append(opts.Styles, protocol.TextStyle{
					Start: span.Start,
					Len:   span.Len,
					St:    span.St,
				})
			}
		}
		if strings.TrimSpace(opts.Text) == "" {
			continue
		}
		if !sentFirst {
			opts.Quote = quote
			sentFirst = true
		}
		if c.config.MentionsEnabled && threadType == protocol.ThreadTypeGroup {
			opts.Mentions = c.resolveMentions(ctx, chatID, opts.Text)
		}

		if err := c.waitSend(ctx, chatID); err != nil {
			return err
		}

		err := retryTransient(ctx, func() error {
			_, err := protocol.SendMessageOptions(ctx, sess, chatID, threadType, opts)
			return err
		})
		if err != nil {
			c.handleSendError(err)
			return err
		}

		if i < len(chunks)-1 && c.limiter != nil {
			if err := c.limiter.Jitter(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// retryTransient retries only transient failures, twice, with the documented
// 1s/3s backoff. Auth and policy errors return immediately.
func retryTransient(ctx context.Context, fn func() error) error {
	err := fn()
	for _, delay := range sendRetryDelays {
		if err == nil || !protocol.IsTransient(err) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if sleepErr := sleepContext(ctx, delay); sleepErr != nil {
			return sleepErr
		}
		err = fn()
	}
	return err
}
