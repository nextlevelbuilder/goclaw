package tools

import (
	"strings"
	"sync"
	"time"
)

// NotifyRoutingMeta carries routing info for batched team notifications.
type NotifyRoutingMeta struct {
	Mode      string // "direct" or "leader"
	Channel   string
	ChatID    string
	UserID    string
	LeadAgent string // agent key (only used in leader mode)
}

// TeamNotifyQueue batches team task notifications per chat with debounce,
// following the same pattern as AnnounceQueue for subagent results.
type TeamNotifyQueue struct {
	mu       sync.Mutex
	batches  map[string]*notifyBatch // key: "teamID:chatID"
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
		batches:  make(map[string]*notifyBatch),
		debounce: time.Duration(debounceMs) * time.Millisecond,
		cap:      20,
		onDrain:  onDrain,
	}
}

// Enqueue adds a formatted notification line to the batch for the given key.
// Resets debounce timer. Drains immediately if cap is reached.
func (q *TeamNotifyQueue) Enqueue(key string, content string, meta NotifyRoutingMeta) {
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
