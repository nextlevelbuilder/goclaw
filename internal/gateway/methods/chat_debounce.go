package methods

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
)

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

func (d *chatDebouncer) Push(key string, delay time.Duration, item chatSendRequest) {
	if delay <= 0 {
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

func chatDebounceDelay(cfg *config.Config) time.Duration {
	debounceMs := 0
	if cfg != nil {
		debounceMs = cfg.Gateway.InboundDebounceMs
	}
	if debounceMs == 0 {
		debounceMs = 1000
	}
	return time.Duration(debounceMs) * time.Millisecond
}

func chatDebounceKey(userID, sessionKey string) string {
	return userID + ":" + sessionKey
}
