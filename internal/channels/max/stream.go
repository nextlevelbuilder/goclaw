package max

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// =====================================================================
// Tunables
// =====================================================================

// streamThrottleInterval is the minimum delay between PUT /messages edits
// during a single streaming run. Chosen to balance UX (responsive) vs API
// pressure (Max enforces 30 rps per bot; 800ms = 1.25 rps even at full
// utilization, leaving headroom for parallel runs).
const streamThrottleInterval = 800 * time.Millisecond

// streamPlaceholderText is the initial text sent when a stream is created.
// Shown to the user immediately so they know the bot has received their
// message and is generating a response.
const streamPlaceholderText = "💭 Thinking..."

// streamMaxBytes caps the size of any single stream edit. Matches Max's
// per-message text limit. We never split a streaming message — long content
// is shown via final Send() which performs proper chunking.
const streamMaxBytes = maxMessageBytes // from format.go

// =====================================================================
// maxStream — implements channels.ChannelStream
// =====================================================================

// maxStream is a single-run streaming handle for the Max channel.
//
// Lifecycle:
//
//	NEW (placeholder created in CreateStream) →
//	  Update(text)... — throttled edits via PUT /messages
//	→ Stop() — final flush of any pending text
//	→ FinalizeStream() — hands messageID back to the channel's placeholders
//	  map so Send() can perform the final markdown-formatted edit.
//
// Concurrency:
//   - Update/Stop are safe to call concurrently with each other (mu guards).
//   - One maxStream instance per agent run; not shared across runs.
type maxStream struct {
	client *Client
	chatID int64

	mu        sync.Mutex
	messageID string    // mid.* returned by initial SendMessage; empty until set
	pending   string    // text accumulated since last edit; cleared on flush
	lastSent  string    // last text actually sent — used for dedup
	lastEdit  time.Time // last successful edit timestamp
	stopped   bool      // true after Stop has been called
	createErr error     // non-nil if placeholder creation failed; subsequent ops no-op
}

// Update is called by the agent loop with the latest accumulated text.
// Throttles edits to streamThrottleInterval; if the throttle window has not
// elapsed, the text is buffered as `pending` and sent on the next Update or
// Stop.
//
// No error is returned per the channels.ChannelStream contract — failures
// are logged at debug level and the next call retries with newer text.
func (s *maxStream) Update(ctx context.Context, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// No-op if Stop already called or placeholder creation failed.
	if s.stopped || s.createErr != nil {
		return
	}

	// Truncate to Max's per-message limit. If the agent generates more,
	// the user sees a truncated preview while streaming; the final Send()
	// will deliver the full content with proper chunking.
	if len(text) > streamMaxBytes {
		text = text[:safeUTF8Cut(text, streamMaxBytes)]
	}

	// Dedup: identical text → nothing to do (avoids sending unchanged edits
	// when the agent emits the same accumulated text twice).
	if text == s.lastSent {
		return
	}

	// Buffer the latest text. The accumulator pattern means later updates
	// supersede earlier ones — we never edit with stale content.
	s.pending = text

	// Throttle: if the window hasn't elapsed, defer to the next call.
	// `pending` will be picked up there (or in Stop's final flush).
	if time.Since(s.lastEdit) < streamThrottleInterval {
		return
	}

	// Edit window open — flush now.
	s.flushLocked(ctx)
}

// Stop performs a final flush of any pending text and marks the stream as
// closed. Subsequent Update calls become no-ops. The placeholder message is
// NOT deleted — Send() will edit it with the final formatted response after
// FinalizeStream hands off the messageID.
//
// Returns nil even on flush failure: the stream contract is best-effort UX,
// and Send() will deliver the correct content regardless.
func (s *maxStream) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return nil
	}
	s.stopped = true

	// Final flush: deliver any buffered text the throttle window swallowed.
	if s.pending != "" && s.pending != s.lastSent && s.createErr == nil {
		s.flushLocked(ctx)
	}
	return nil
}

// MessageID returns 0 — Max uses string mid.* identifiers, not int IDs.
// FinalizeStream uses the typed messageIDStr method below for the actual
// handoff to placeholders.
//
// Returning 0 here is intentional and matches Slack's pattern: the
// channels.ChannelStream interface predates string-keyed platforms.
func (s *maxStream) MessageID() int {
	return 0
}

// messageIDStr returns the Max message_id ("mid.xxx") for FinalizeStream's
// channel-specific handoff. Distinct method name avoids interface collision.
func (s *maxStream) messageIDStr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messageID
}

// flushLocked performs one PUT /messages edit. Caller must hold s.mu.
//
// Failures are logged at debug and not propagated — the next Update will
// retry with a fresher text snapshot. This trades exactness for UX: a
// single dropped edit is invisible at 800ms cadence, but a propagated
// error would freeze the stream for the rest of the run.
//
// Plain text is sent (no `format` field) to avoid mid-token markdown
// rendering glitches (e.g. "**bold" with unclosed asterisks).
// The final Send() applies markdown after the run completes.
func (s *maxStream) flushLocked(ctx context.Context) {
	if s.messageID == "" {
		// Defensive: should not happen because CreateStream populates this.
		// If it does (e.g. placeholder creation failed silently), skip.
		return
	}
	text := s.pending
	if text == "" || text == s.lastSent {
		return
	}

	// Panic safety: if EditMessage panics (e.g. transport bug, custom
	// slog handler crash), don't bring down the run. Log + swallow so
	// the next Update can retry. Stream is best-effort UX; final Send
	// will deliver the actual response with proper formatting.
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("max: panic in stream flush (recovered)",
				"chat_id", s.chatID, "message_id", s.messageID, "panic", r)
		}
	}()

	_, err := s.client.EditMessage(ctx, EditMessageParams{
		MessageID: s.messageID,
		Body: EditMessageRequest{
			Text: text,
			// format omitted — plain text during stream.
		},
	})
	if err != nil {
		// Don't propagate — stream is best-effort. Log at debug to keep
		// production logs clean (these errors are common on flaky networks
		// and self-recover via the next Update).
		slog.Debug("max: stream edit failed (will retry on next update)",
			"chat_id", s.chatID, "error", err)
		return
	}

	s.lastSent = text
	s.lastEdit = time.Now()
	s.pending = "" // consumed
}

// Compile-time check: maxStream must satisfy channels.ChannelStream.
var _ channels.ChannelStream = (*maxStream)(nil)

// =====================================================================
// StreamingChannel implementation — methods on *Channel
// =====================================================================

// StreamEnabled reports whether streaming is active for this chat type.
// Implements channels.StreamingChannel.
//
// Defaults:
//   - DMs: true (modern UX expected)
//   - Groups: false (Max platform doesn't yet support bots in groups; even
//     once it does, streaming in groups is more visually noisy and should
//     be opt-in)
//
// Per-instance overrides via cfg.DMStream / cfg.GroupStream.
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if isGroup {
		if c.cfg.GroupStream != nil {
			return *c.cfg.GroupStream
		}
		return false
	}
	if c.cfg.DMStream != nil {
		return *c.cfg.DMStream
	}
	return true
}

// CreateStream creates a per-run streaming handle for the given chatID.
// Implements channels.StreamingChannel.
//
// Sends a placeholder message ("💭 Thinking...") immediately and stores its
// message_id on the returned stream. Subsequent Update calls edit this
// message in place.
//
// firstStream is currently ignored — Max channel uses the same stream type
// for both reasoning and answer lanes (Опция 2). When ReasoningStreamEnabled
// is enabled in a future iteration, firstStream may select between
// "reasoning" and "answer" placeholder text.
//
// Returns a non-nil stream even on placeholder creation failure: a
// degraded-mode stream whose Update/Stop are no-ops, so the agent loop
// proceeds without the streaming preview but otherwise functions normally.
// The error returned helps the caller log the issue.
func (c *Channel) CreateStream(ctx context.Context, chatID string, firstStream bool) (channels.ChannelStream, error) {
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || chatIDInt == 0 {
		return nil, errors.New("max stream: invalid chat ID " + chatID)
	}

	stream := &maxStream{
		client: c.client,
		chatID: chatIDInt,
	}

	// Send placeholder eagerly. Failure here doesn't fail the run — we
	// return a stream with createErr set so Update/Stop become no-ops, and
	// the agent loop continues.
	resp, err := c.client.SendMessage(ctx, SendMessageParams{
		ChatID: chatIDInt,
		Body: SendMessageRequest{
			Text: streamPlaceholderText,
			// No format — placeholder is plain text.
		},
	})
	if err != nil {
		stream.createErr = err
		slog.Warn("max: stream placeholder creation failed (run continues without streaming)",
			"channel", c.Name(), "chat_id", chatID, "error", err)
		// Return the stream + error: the agent loop will log and continue.
		// Returning (nil, err) would also be valid; channels API allows both.
		// We choose to return the stream to keep the caller code simpler.
		return stream, err
	}

	stream.messageID = resp.MessageID
	stream.lastEdit = time.Now() // start the throttle window

	slog.Debug("max: stream created",
		"channel", c.Name(), "chat_id", chatID, "message_id", resp.MessageID)

	return stream, nil
}

// FinalizeStream hands the stream's message_id back to the channel's
// placeholders map so that Send() can perform the final markdown-formatted
// edit instead of sending a new message.
// Implements channels.StreamingChannel.
//
// If the stream had no messageID (placeholder creation failed), this is a
// no-op — Send() will fall back to sending a fresh message with the answer.
//
// If the stream HAD a messageID but never produced any content (lastSent
// empty — agent crashed or errored before the first Update), we delete
// the placeholder rather than handing off. Reasoning: if Send is later
// called (e.g. with an error message), it will fresh-send and the user
// gets one clean message instead of "💭 Thinking..." then an error reply.
// If Send is NOT called (worst case: agent crash without recovery), the
// orphan "💭 Thinking..." would otherwise live in the chat indefinitely.
func (c *Channel) FinalizeStream(ctx context.Context, chatID string, stream channels.ChannelStream) {
	ms, ok := stream.(*maxStream)
	if !ok {
		return
	}

	ms.mu.Lock()
	mid := ms.messageID
	everSent := ms.lastSent != ""
	ms.mu.Unlock()

	if mid == "" {
		return
	}

	if !everSent {
		// Orphan placeholder — best-effort delete.
		slog.Info("max: stream had no content, deleting placeholder",
			"channel", c.Name(), "chat_id", chatID, "message_id", mid)
		if err := c.client.DeleteMessage(ctx, mid); err != nil {
			slog.Debug("max: orphan placeholder delete failed (non-fatal)",
				"channel", c.Name(), "message_id", mid, "error", err)
		}
		return
	}

	c.placeholders.Store(chatID, mid)

	slog.Info("max: stream finalized, handing off to Send()",
		"channel", c.Name(), "chat_id", chatID, "message_id", mid)
}

// ReasoningStreamEnabled returns whether reasoning content should be shown
// as a separate streaming message. Implements channels.StreamingChannel.
//
// Returns false: Max channel uses Опция 2 (answer-only streaming).
// Reasoning content is not surfaced to users in this iteration.
//
// A future enhancement (Опция 3) could enable reasoning lanes by:
//  1. Returning true here.
//  2. Differentiating placeholder text in CreateStream based on firstStream.
//  3. Adjusting outbound.go to handle the case where placeholders contains
//     a reasoning message that should be preserved (not edited over) when
//     the answer lane begins.
func (c *Channel) ReasoningStreamEnabled() bool {
	return false
}
