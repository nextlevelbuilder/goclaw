package tools

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// NotifyRoutingMeta carries routing info for batched team notifications.
//
// Batching keys off the *entire* routing tuple (see notifyBatchKey), so two
// notifications only merge into one batch when every routing dimension matches.
// The previous opaque "teamID:chatID" string key could collide across tenants,
// peer kinds, forum topics, target users, or leader agents whenever a chat ID
// happened to contain a colon or two distinct scopes shared a chat ID.
type NotifyRoutingMeta struct {
	TenantID  uuid.UUID // tenant scope from the authoritative event
	TeamID    string    // team UUID string
	Mode      string    // "direct" or "leader"
	Channel   string
	ChatID    string
	UserID    string
	LeadAgent string // agent key (only used in leader mode)
	PeerKind  string // "group" or "direct" — routes to correct session (#266)
	LocalKey  string // composite key with topic suffix for forum routing
}

// notifyBatchKey is the comparable batching key derived from a normalized
// NotifyRoutingMeta. It is an ordinary struct (all comparable fields) so it can
// key a map directly — no delimiter concatenation, so values containing ':'
// (Telegram forum local keys, composite chat IDs) can never collide.
type notifyBatchKey struct {
	tenantID  uuid.UUID
	teamID    string
	mode      string
	channel   string
	peerKind  string
	chatID    string
	localKey  string
	userID    string
	leadAgent string
}

// normalizeNotifyMeta trims all fields and lowercases only the enum-like
// dimensions (mode, channel, peerKind). Opaque identifiers (chat/local/user/
// agent) are preserved verbatim — lowercasing them would merge distinct scopes.
func normalizeNotifyMeta(meta NotifyRoutingMeta) NotifyRoutingMeta {
	meta.TeamID = strings.TrimSpace(meta.TeamID)
	meta.Mode = strings.ToLower(strings.TrimSpace(meta.Mode))
	meta.Channel = strings.ToLower(strings.TrimSpace(meta.Channel))
	meta.PeerKind = strings.ToLower(strings.TrimSpace(meta.PeerKind))
	meta.ChatID = strings.TrimSpace(meta.ChatID)
	meta.LocalKey = strings.TrimSpace(meta.LocalKey)
	meta.UserID = strings.TrimSpace(meta.UserID)
	meta.LeadAgent = strings.TrimSpace(meta.LeadAgent)
	return meta
}

func notifyKeyOf(meta NotifyRoutingMeta) notifyBatchKey {
	return notifyBatchKey{
		tenantID:  meta.TenantID,
		teamID:    meta.TeamID,
		mode:      meta.Mode,
		channel:   meta.Channel,
		peerKind:  meta.PeerKind,
		chatID:    meta.ChatID,
		localKey:  meta.LocalKey,
		userID:    meta.UserID,
		leadAgent: meta.LeadAgent,
	}
}

// TeamNotifyQueue batches team task notifications per full routing tuple with
// debounce, following the same pattern as AnnounceQueue for subagent results.
type TeamNotifyQueue struct {
	mu       sync.Mutex
	batches  map[notifyBatchKey]*notifyBatch
	debounce time.Duration
	cap      int // immediate drain threshold
	onDrain  func(items []string, meta NotifyRoutingMeta)
}

type notifyBatch struct {
	items []string
	timer *time.Timer
	meta  NotifyRoutingMeta
}

// NewTeamNotifyQueue creates a notification queue with debounce and drain callback.
func NewTeamNotifyQueue(debounceMs int, onDrain func(items []string, meta NotifyRoutingMeta)) *TeamNotifyQueue {
	if debounceMs <= 0 {
		debounceMs = 2000
	}
	return &TeamNotifyQueue{
		batches:  make(map[notifyBatchKey]*notifyBatch),
		debounce: time.Duration(debounceMs) * time.Millisecond,
		cap:      20,
		onDrain:  onDrain,
	}
}

// Enqueue adds a formatted notification line to the batch keyed by the full
// normalized routing tuple. Resets the debounce timer. Drains immediately if
// cap is reached. The first enqueue for a key freezes the batch metadata; only
// notifications whose entire routing tuple matches merge into it.
func (q *TeamNotifyQueue) Enqueue(content string, meta NotifyRoutingMeta) {
	meta = normalizeNotifyMeta(meta)
	key := notifyKeyOf(meta)

	q.mu.Lock()
	defer q.mu.Unlock()

	b, ok := q.batches[key]
	if !ok {
		b = &notifyBatch{meta: meta}
		q.batches[key] = b
	}
	b.items = append(b.items, content)

	// Immediate drain if cap reached.
	if len(b.items) >= q.cap {
		if b.timer != nil {
			b.timer.Stop()
		}
		items := b.items
		bMeta := b.meta
		delete(q.batches, key)
		go q.drain(items, bMeta)
		return
	}

	// Reset debounce timer.
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(q.debounce, func() {
		q.mu.Lock()
		b, ok := q.batches[key]
		if !ok {
			q.mu.Unlock()
			return
		}
		items := b.items
		bMeta := b.meta
		delete(q.batches, key)
		q.mu.Unlock()

		q.drain(items, bMeta)
	})
}

func (q *TeamNotifyQueue) drain(items []string, meta NotifyRoutingMeta) {
	if len(items) == 0 || q.onDrain == nil {
		return
	}
	q.onDrain(items, meta)
}

// FormatBatchedNotify joins notification lines into a single message.
func FormatBatchedNotify(items []string) string {
	return strings.Join(items, "\n")
}
