package telegram

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mymmrac/telego"
)

// Telegram delivers an album (multiple media items grouped on the client) as
// N separate Message updates, each carrying the same MediaGroupID. This
// aggregator buffers album members at the channel layer so downstream code
// sees ONE synthesized message per album — eliminating the original bug where
// N concurrent agent runs replied N times to a single user action.
//
// Contract (see plans/260528-1351-multi-attachment-debounce/phase-02 Rules):
//   - Buffer key is (chatID, MediaGroupID). SenderID is pinned at buffer
//     creation. A subsequent Push to the same key with a different senderID
//     is dropped with a security warn (Rule #3) — Telegram does not reuse
//     MediaGroupID across senders, but defense-in-depth catches spoofed
//     updates. Plan Rule #2 originally specified a 3-tuple key including
//     senderID; that made the rebind defense unreachable, so the 2-tuple
//     key is used here with sender-pin as the runtime tripwire.
//   - The first Push pins the representative's resolvedMessageContext on
//     the buffer (Rule #5). Subsequent pushes pass their own rctx but it is
//     discarded — only members[0]'s reply/thread/topic context flows downstream.
//   - Caller MUST gate empty MediaGroupID; Push returns false if absent.
//   - On every arrival the silence timer is Stop()+replaced via AfterFunc.
//     No time.Timer.Reset() — avoids the documented double-fire race (Rule #7).
//   - Dual caps with drop-and-log overflow (Rule #6).
//   - Stop() flushes all pending buffers synchronously. Push after Stop is
//     rejected (Rule #8 / post-shutdown straggler).
const (
	albumAggregatorWindow      = 500 * time.Millisecond
	albumAggregatorMaxBuffered = 100  // per album buffer (Telegram caps at 10; defensive)
	albumAggregatorMaxBuffers  = 1000 // global active buffers (DoS guard)
)

type albumAggregator struct {
	window     time.Duration
	maxPerBuf  int
	maxBuffers int
	flushFn    func(repCtx resolvedMessageContext, members []*telego.Message)

	mu      sync.Mutex
	buffers map[string]*albumBuffer
	stopped bool
}

type albumBuffer struct {
	repCtx   resolvedMessageContext // captured from members[0]
	members  []*telego.Message
	senderID int64 // pinned on first Push
	timer    *time.Timer
}

func newAlbumAggregator(window time.Duration, maxPerBuf, maxBuffers int, flushFn func(resolvedMessageContext, []*telego.Message)) *albumAggregator {
	return &albumAggregator{
		window:     window,
		maxPerBuf:  maxPerBuf,
		maxBuffers: maxBuffers,
		flushFn:    flushFn,
		buffers:    make(map[string]*albumBuffer),
	}
}

// albumKey returns the buffer key plus the sender ID. ok=false if the
// message is missing required fields (no MediaGroupID, no From). Caller must
// short-circuit when ok=false.
func albumKey(msg *telego.Message) (key string, senderID int64, ok bool) {
	if msg == nil || msg.MediaGroupID == "" || msg.From == nil {
		return "", 0, false
	}
	return fmt.Sprintf("%d:%s", msg.Chat.ID, msg.MediaGroupID), msg.From.ID, true
}

// Push accepts an album member. Returns true if accepted, false otherwise
// (empty MediaGroupID, post-stop, global overflow, sender-rebind, per-buffer
// overflow). On false the caller MUST fall through to single-message dispatch
// so the message is not silently lost.
//
// rctx is the resolved-message context for THIS message; it is stored only
// on the first Push for a key (members[0] is the representative per Rule #5).
func (a *albumAggregator) Push(msg *telego.Message, rctx resolvedMessageContext) bool {
	key, senderID, ok := albumKey(msg)
	if !ok {
		return false
	}

	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		slog.Warn("telegram.album_post_shutdown_push", "key", key, "message_id", msg.MessageID)
		return false
	}

	buf, exists := a.buffers[key]
	if !exists {
		if len(a.buffers) >= a.maxBuffers {
			a.mu.Unlock()
			slog.Warn("telegram.album_overflow",
				"scope", "global", "max", a.maxBuffers, "key", key)
			return false
		}
		buf = &albumBuffer{senderID: senderID, repCtx: rctx}
		a.buffers[key] = buf
	} else {
		if buf.senderID != senderID {
			a.mu.Unlock()
			slog.Warn("security.album_sender_mismatch",
				"key", key, "expected_sender", buf.senderID, "got_sender", senderID,
				"message_id", msg.MessageID)
			return false
		}
		if len(buf.members) >= a.maxPerBuf {
			a.mu.Unlock()
			slog.Warn("telegram.album_overflow",
				"scope", "buffer", "max", a.maxPerBuf, "key", key, "message_id", msg.MessageID)
			return false
		}
	}

	buf.members = append(buf.members, msg)
	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.timer = time.AfterFunc(a.window, func() { a.flushKey(key) })
	a.mu.Unlock()
	return true
}

// flushKey drains the named buffer and invokes flushFn outside the lock.
// Safe to call multiple times — second call is a no-op.
func (a *albumAggregator) flushKey(key string) {
	a.mu.Lock()
	buf, ok := a.buffers[key]
	if !ok {
		a.mu.Unlock()
		return
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	members := buf.members
	repCtx := buf.repCtx
	delete(a.buffers, key)
	a.mu.Unlock()

	if len(members) == 0 {
		return
	}
	a.flushFn(repCtx, members)
}

// Stop marks the aggregator as stopped and synchronously flushes all pending
// buffers. After Stop, Push returns false and logs a warn. Idempotent.
func (a *albumAggregator) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	keys := make([]string, 0, len(a.buffers))
	for k := range a.buffers {
		keys = append(keys, k)
	}
	a.mu.Unlock()

	for _, k := range keys {
		a.flushKey(k)
	}
}
