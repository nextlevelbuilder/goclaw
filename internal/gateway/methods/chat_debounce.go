package methods

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// chatMediaDebounceFloorMs is the minimum debounce window applied to Web Chat
// sends that carry media when the post-override delay would otherwise be 0.
// Mirrors cmd/gateway_consumer_debounce.go mediaDebounceFloorMs (Phase 1) —
// duplicated by value (1000ms) to keep gateway/methods decoupled from cmd.
const chatMediaDebounceFloorMs = 1000

type chatSendRequest struct {
	ctx        context.Context
	client     *gateway.Client
	requestID  string
	params     chatSendParams
	loop       agent.Agent
	userID     string
	sessionKey string
}

type chatDebouncer struct {
	mu      sync.Mutex
	buffers map[string]*chatDebounceBuffer
	flushFn func([]chatSendRequest)
}

type chatDebounceBuffer struct {
	items []chatSendRequest
	timer *time.Timer
}

func newChatDebouncer(flushFn func([]chatSendRequest)) *chatDebouncer {
	return &chatDebouncer{
		buffers: make(map[string]*chatDebounceBuffer),
		flushFn: flushFn,
	}
}

// Push appends an item to the per-key buffer.
//
// Behavior:
//   - delay > 0: append + (re)set the silence timer (existing buffered path).
//   - delay <= 0 AND a buffer already exists with items: append the incoming
//     item to the buffer and flush immediately (merge-then-flush). Required so
//     a no-media follow-up cannot bypass a buffered media chat-send and trigger
//     a duplicate dispatch (Phase 1.5 Rule #4, mirrors bus debouncer Rule #1).
//   - delay <= 0 AND no buffer exists: dispatch immediately (passthrough).
func (d *chatDebouncer) Push(key string, delay time.Duration, item chatSendRequest) {
	if delay <= 0 {
		d.mu.Lock()
		buf, exists := d.buffers[key]
		if exists && len(buf.items) > 0 {
			buf.items = append(buf.items, item)
			if buf.timer != nil {
				buf.timer.Stop()
			}
			items := buf.items
			delete(d.buffers, key)
			d.mu.Unlock()
			d.flushFn(items)
			return
		}
		d.mu.Unlock()
		d.flushFn([]chatSendRequest{item})
		return
	}

	d.mu.Lock()
	buf, exists := d.buffers[key]
	if !exists {
		buf = &chatDebounceBuffer{}
		d.buffers[key] = buf
	}
	buf.items = append(buf.items, item)
	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.timer = time.AfterFunc(delay, func() {
		d.Flush(key)
	})
	d.mu.Unlock()
}

func (d *chatDebouncer) Flush(key string) {
	items := d.Take(key)
	if len(items) == 0 {
		return
	}
	d.flushFn(items)
}

func (d *chatDebouncer) Take(key string) []chatSendRequest {
	d.mu.Lock()
	buf, ok := d.buffers[key]
	if !ok || len(buf.items) == 0 {
		d.mu.Unlock()
		return nil
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	items := buf.items
	delete(d.buffers, key)
	d.mu.Unlock()

	return items
}

func (d *chatDebouncer) Discard(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	buf, ok := d.buffers[key]
	if !ok {
		return
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	delete(d.buffers, key)
}

func (d *chatDebouncer) Stop() {
	d.mu.Lock()
	keys := make([]string, 0, len(d.buffers))
	for key := range d.buffers {
		keys = append(keys, key)
	}
	d.mu.Unlock()

	for _, key := range keys {
		d.Flush(key)
	}
}

func mergeChatSendRequests(items []chatSendRequest) chatSendParams {
	if len(items) == 0 {
		return chatSendParams{}
	}
	last := items[len(items)-1].params
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.params.Message != "" {
			parts = append(parts, item.params.Message)
		}
	}
	last.Message = strings.Join(parts, "\n")
	return last
}

// chatDebounceDelay computes the per-send debounce window.
//
// Precedence: agent override (when set) overrides the global config. The media
// floor fires ONLY when the post-override delay is exactly 0 AND the message
// carries media — a non-zero agent override (even below the floor) is honored
// verbatim. Mirrors Phase 1's resolveInboundDebounceDelay + applyMediaFloor.
func chatDebounceDelay(cfg *config.Config, agentOtherConfig json.RawMessage, hasMedia bool) time.Duration {
	debounceMs := 0
	if cfg != nil {
		debounceMs = cfg.Gateway.InboundDebounceMs
	}
	if overrideMs, ok := store.ParseInboundDebounceMsFromOtherConfig(agentOtherConfig); ok {
		debounceMs = overrideMs
	}
	if debounceMs <= 0 && hasMedia {
		debounceMs = chatMediaDebounceFloorMs
	}
	if debounceMs <= 0 {
		return 0
	}
	return time.Duration(debounceMs) * time.Millisecond
}

func chatDebounceKey(userID, sessionKey string) string {
	return userID + ":" + sessionKey
}
