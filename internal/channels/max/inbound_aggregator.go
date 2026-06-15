package max

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Max delivers a logically-single user action (e.g. "send text + attach
// file together") as N separate Update events arriving within ~50–500ms
// of each other. Each event currently reaches handleMessage independently
// and triggers its own agent run — producing 2 disjoint responses for
// what the user perceived as one message: a stale response based on the
// text (no file in context), then a delayed response with the file.
//
// This aggregator coalesces Max inbound events at the channel layer.
// Each Push delays dispatch by aggregatorWindow. Subsequent Push within
// the window for the same (chat, sender) extends the silence timer and
// appends to the buffer. When the timer fires, the buffered messages are
// merged into ONE synthesized inbound and dispatched downstream — so
// only one agent run sees all parts of the user action.
//
// Contract (deliberately mirrors telegram/album_aggregator.go):
//   - Buffer key is (chatID, senderID). Unlike Telegram (where the user-
//     client supplies an explicit MediaGroupID), Max has no group ID, so
//     we key on the conversation+sender 2-tuple. Defense: a Push with a
//     different senderID for the same chatID would land in a separate
//     bucket — there is no cross-sender contamination risk by design.
//   - On every arrival the silence timer is Stop()+replaced via AfterFunc.
//     No time.Timer.Reset() — avoids the documented double-fire race.
//   - Dual DoS caps: max messages per buffer + max active buffers.
//   - Stop() flushes all pending buffers synchronously. Push after Stop
//     is rejected (post-shutdown straggler) so flushFn is never called
//     after caller has begun teardown.
//
// Why this exists despite a shared bus-level debouncer:
//   The shared internal/bus/inbound_debounce.go has a media-bypass
//   shortcut (in deployed versions before upstream f771cff7): when a
//   subsequent message carries media, it flushes the buffer and passes
//   through immediately. That race triggers the duplicate-run bug for
//   Max. Coalescing one layer earlier — at the channel — sidesteps the
//   bypass entirely.

const (
	aggregatorWindow      = 800 * time.Millisecond
	aggregatorMaxPerBuf   = 100  // messages per buffer (defensive)
	aggregatorMaxBuffers  = 1000 // global active buffers (DoS guard)
)

// AggregatedMessage carries the original Message plus the context (ctx,
// edited flag) the handler needs to dispatch it. We capture the per-Push
// context so flushFn can recreate the exact dispatch the original handler
// would have made.
type aggregatedMessage struct {
	ctx    context.Context
	msg    Message
	edited bool
}

// inboundAggregator coalesces rapid Max inbound events from the same
// (chat, sender) into a single merged dispatch.
type inboundAggregator struct {
	window     time.Duration
	maxPerBuf  int
	maxBuffers int

	// flushFn receives the merged "representative" message after the
	// silence window elapses. The representative is built by
	// mergeAggregated() — it has merged content/attachments/metadata
	// from all buffered members. ctx is from members[0] (Rule: first
	// message pins context).
	flushFn func(ctx context.Context, merged Message, edited bool)

	mu      sync.Mutex
	buffers map[string]*aggregatorBuffer
	stopped bool
}

type aggregatorBuffer struct {
	members []aggregatedMessage
	timer   *time.Timer
}

func newInboundAggregator(window time.Duration, maxPerBuf, maxBuffers int, flushFn func(context.Context, Message, bool)) *inboundAggregator {
	return &inboundAggregator{
		window:     window,
		maxPerBuf:  maxPerBuf,
		maxBuffers: maxBuffers,
		flushFn:    flushFn,
		buffers:    make(map[string]*aggregatorBuffer),
	}
}

// aggregatorKey returns the per-buffer key. ok=false if the message lacks
// required fields (sender or recipient). Caller must short-circuit when
// ok=false (those messages should not reach handleMessage in the first
// place — defense in depth).
func aggregatorKey(msg Message) (key string, ok bool) {
	if msg.Sender == nil || msg.Recipient == nil {
		return "", false
	}
	return fmt.Sprintf("%d:%d", msg.Recipient.ChatID, msg.Sender.UserID), true
}

// Push appends a message to the per-(chat,sender) buffer and (re)arms the
// silence timer. Returns true if the message was accepted into the
// aggregator (caller MUST NOT also dispatch directly). Returns false if
// the message was rejected (caller MUST fall through to direct dispatch
// so the message is not silently dropped).
//
// Rejection cases:
//   - aggregator was Stopped (shutdown straggler)
//   - missing sender/recipient (defense in depth — should not happen)
//   - global buffer overflow (DoS guard)
//   - per-buffer overflow (DoS guard)
func (a *inboundAggregator) Push(ctx context.Context, msg Message, edited bool) bool {
	key, ok := aggregatorKey(msg)
	if !ok {
		return false
	}

	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		slog.Warn("max: aggregator push after stop",
			"key", key, "edited", edited)
		return false
	}

	buf, exists := a.buffers[key]
	if !exists {
		if len(a.buffers) >= a.maxBuffers {
			a.mu.Unlock()
			slog.Warn("max: aggregator overflow",
				"scope", "global", "max", a.maxBuffers, "key", key)
			return false
		}
		buf = &aggregatorBuffer{}
		a.buffers[key] = buf
	} else if len(buf.members) >= a.maxPerBuf {
		a.mu.Unlock()
		slog.Warn("max: aggregator overflow",
			"scope", "buffer", "max", a.maxPerBuf, "key", key)
		return false
	}

	buf.members = append(buf.members, aggregatedMessage{ctx: ctx, msg: msg, edited: edited})
	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.timer = time.AfterFunc(a.window, func() { a.flushKey(key) })
	a.mu.Unlock()
	return true
}

// flushKey drains the named buffer and invokes flushFn outside the lock.
// Safe to call multiple times — second call is a no-op.
func (a *inboundAggregator) flushKey(key string) {
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
	delete(a.buffers, key)
	a.mu.Unlock()

	if len(members) == 0 {
		return
	}

	merged, edited := mergeAggregated(members)
	a.flushFn(members[0].ctx, merged, edited)
}

// Stop marks the aggregator as stopped and synchronously flushes all
// pending buffers. After Stop, Push returns false and logs a warn.
// Idempotent.
func (a *inboundAggregator) Stop() {
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

// mergeAggregated builds the synthesized representative Message from a
// list of aggregated members. Strategy:
//
//   - Take members[0] as the base (its Sender, Recipient, Timestamp,
//     Link are authoritative — first message pins identity).
//   - Concatenate non-empty Body.Text values from all members with
//     "\n" separator (matches shared mergeInboundMessages convention).
//   - Concatenate Body.Attachments arrays in arrival order so the
//     downstream media download sees ALL attachments from ALL events
//     as one logical inbound.
//   - edited = OR of all member edited flags (if ANY member is an edit,
//     the merged result is considered edited; conservative).
//
// Length-1 input returns the original message untouched.
func mergeAggregated(members []aggregatedMessage) (Message, bool) {
	if len(members) == 0 {
		return Message{}, false
	}
	if len(members) == 1 {
		return members[0].msg, members[0].edited
	}

	base := members[0].msg
	editedAny := members[0].edited

	// Defensive: clone Body to avoid mutating the source message's slice
	// shared with other handlers / logs.
	var mergedBody MessageBody
	if base.Body != nil {
		mergedBody = *base.Body
		if base.Body.Attachments != nil {
			mergedBody.Attachments = append([]Attachment(nil), base.Body.Attachments...)
		}
	}

	var texts []string
	if strings.TrimSpace(mergedBody.Text) != "" {
		texts = append(texts, mergedBody.Text)
	}

	for i := 1; i < len(members); i++ {
		m := members[i].msg
		editedAny = editedAny || members[i].edited
		if m.Body == nil {
			continue
		}
		if t := strings.TrimSpace(m.Body.Text); t != "" {
			texts = append(texts, m.Body.Text)
		}
		if len(m.Body.Attachments) > 0 {
			mergedBody.Attachments = append(mergedBody.Attachments, m.Body.Attachments...)
		}
	}

	if len(texts) > 0 {
		mergedBody.Text = strings.Join(texts, "\n")
	}
	base.Body = &mergedBody

	slog.Debug("max: aggregator merged messages",
		"count", len(members),
		"chat_id", base.Recipient.ChatID,
		"sender_id", base.Sender.UserID,
		"text_len", len(mergedBody.Text),
		"attachments", len(mergedBody.Attachments),
	)

	return base, editedAny
}
