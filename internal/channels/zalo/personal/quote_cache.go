package personal

import (
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const (
	defaultQuoteCacheTTL = 24 * time.Hour
	defaultQuoteCacheMax = 5000
)

type quoteEntry struct {
	msg     protocol.TMessage
	expires time.Time
}

// quoteCache keeps inbound messages so an outbound reply can quote the exact
// source Zalo expects (qmsgId, qmsgCliId, qmsgType, qmsgAttach). TTL matches the
// plan's 24h window; the cap prevents unbounded growth in busy groups.
type quoteCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	now     func() time.Time
	entries map[string]quoteEntry
	order   []string
}

func newQuoteCache(ttl time.Duration, max int, now func() time.Time) *quoteCache {
	if ttl <= 0 {
		ttl = defaultQuoteCacheTTL
	}
	if max <= 0 {
		max = defaultQuoteCacheMax
	}
	if now == nil {
		now = time.Now
	}
	return &quoteCache{
		ttl:     ttl,
		max:     max,
		now:     now,
		entries: make(map[string]quoteEntry),
	}
}

func (q *quoteCache) Store(msg protocol.TMessage) {
	if q == nil {
		return
	}
	keys := []string{msg.MsgID, msg.CliMsgID, msg.GlobalMsgID}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	q.pruneLocked(now)
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, exists := q.entries[key]; !exists {
			q.order = append(q.order, key)
		}
		q.entries[key] = quoteEntry{msg: msg, expires: now.Add(q.ttl)}
	}
	for len(q.entries) > q.max && len(q.order) > 0 {
		oldest := q.order[0]
		q.order = q.order[1:]
		delete(q.entries, oldest)
	}
}

func (q *quoteCache) Get(id string) (protocol.TMessage, bool) {
	if q == nil || id == "" {
		return protocol.TMessage{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	q.pruneLocked(now)
	entry, ok := q.entries[id]
	if !ok {
		return protocol.TMessage{}, false
	}
	if !entry.expires.After(now) {
		delete(q.entries, id)
		return protocol.TMessage{}, false
	}
	return entry.msg, true
}

func (q *quoteCache) Delete(id string) {
	if q == nil || id == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.entries, id)
}

func (q *quoteCache) pruneLocked(now time.Time) {
	if len(q.entries) == 0 {
		return
	}
	kept := q.order[:0]
	for _, key := range q.order {
		entry, ok := q.entries[key]
		if !ok {
			continue
		}
		if !entry.expires.After(now) {
			delete(q.entries, key)
			continue
		}
		kept = append(kept, key)
	}
	q.order = kept
}
