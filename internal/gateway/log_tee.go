package gateway

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	runtimelogs "github.com/nextlevelbuilder/goclaw/internal/logs"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	ringBufferSize = 100
	redactedValue  = "***"
)

// sensitiveKeys are attribute keys whose values are redacted before forwarding.
var sensitiveKeys = []string{
	"key", "token", "secret", "password", "dsn",
	"credential", "authorization", "cookie",
}

// LogTee is a slog.Handler that forwards log records to subscribed WS clients
// while delegating to an underlying handler for normal output.
type LogTee struct {
	inner slog.Handler
	state *logTeeState
	attrs []slog.Attr
}

type logTeeState struct {
	mu      sync.RWMutex
	clients map[string]*logSubscriber

	// Ring buffer of recent entries for replay on subscribe.
	ringMu  sync.RWMutex
	ring    []map[string]any
	ringPos int
	ringFul bool
}

// logSubscriber tracks a client and its requested minimum log level.
type logSubscriber struct {
	client *Client
	level  slog.Level
}

// NewLogTee wraps an existing slog.Handler so log records are also forwarded
// to any WebSocket clients that have started log tailing.
func NewLogTee(inner slog.Handler) *LogTee {
	return &LogTee{
		inner: inner,
		state: &logTeeState{
			clients: make(map[string]*logSubscriber),
			ring:    make([]map[string]any, ringBufferSize),
		},
	}
}

func (t *LogTee) Enabled(ctx context.Context, level slog.Level) bool {
	// Always accept if inner handler wants it.
	if t.inner.Enabled(ctx, level) {
		return true
	}
	// Also accept if any subscriber wants this level (e.g. debug).
	t.state.mu.RLock()
	defer t.state.mu.RUnlock()
	for _, sub := range t.state.clients {
		if level >= sub.level {
			return true
		}
	}
	return false
}

func (t *LogTee) Handle(ctx context.Context, r slog.Record) error {
	// Build entry for WS clients.
	t.state.mu.RLock()
	n := len(t.state.clients)
	t.state.mu.RUnlock()

	needEntry := n > 0 // need to broadcast
	// Always build entry for ring buffer regardless of subscribers.
	entry := t.buildEntry(r)

	// Store in ring buffer.
	t.state.ringMu.Lock()
	t.state.ring[t.state.ringPos] = entry
	t.state.ringPos = (t.state.ringPos + 1) % ringBufferSize
	if t.state.ringPos == 0 {
		t.state.ringFul = true
	}
	t.state.ringMu.Unlock()

	// Forward to subscribers.
	if needEntry {
		evt := protocol.NewEvent("log", entry)
		level := r.Level

		t.state.mu.RLock()
		for _, sub := range t.state.clients {
			if level >= sub.level {
				sub.client.SendEvent(*evt)
			}
		}
		t.state.mu.RUnlock()
	}

	// Forward to inner handler only if it accepts this level.
	if t.inner.Enabled(ctx, r.Level) {
		return t.inner.Handle(ctx, r)
	}
	return nil
}

// buildEntry creates the WS payload from a log record, redacting sensitive attrs.
func (t *LogTee) buildEntry(r slog.Record) map[string]any {
	entry := map[string]any{
		"timestamp": r.Time.UnixMilli(),
		"level":     levelName(r.Level),
		"message":   r.Message,
	}

	attrs := map[string]any{}
	for _, a := range t.attrs {
		applyLogAttr(entry, attrs, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		applyLogAttr(entry, attrs, a)
		return true
	})

	if len(attrs) > 0 {
		entry["attrs"] = attrs
	}

	return entry
}

func (t *LogTee) WithAttrs(attrs []slog.Attr) slog.Handler {
	nextAttrs := make([]slog.Attr, 0, len(t.attrs)+len(attrs))
	nextAttrs = append(nextAttrs, t.attrs...)
	nextAttrs = append(nextAttrs, attrs...)
	return &LogTee{
		inner: t.inner.WithAttrs(attrs),
		state: t.state,
		attrs: nextAttrs,
	}
}

func (t *LogTee) WithGroup(name string) slog.Handler {
	return &LogTee{
		inner: t.inner.WithGroup(name),
		state: t.state,
		attrs: append([]slog.Attr(nil), t.attrs...),
	}
}

// Subscribe adds a client to the log tailing set at the given level.
// Pass slog.LevelInfo for default, slog.LevelDebug for verbose.
func (t *LogTee) Subscribe(client *Client, level slog.Level) {
	t.state.mu.Lock()
	t.state.clients[client.ID()] = &logSubscriber{client: client, level: level}
	t.state.mu.Unlock()

	// Replay ring buffer entries at the requested level.
	t.state.ringMu.RLock()
	var entries []map[string]any
	if t.state.ringFul {
		// Buffer is full — read from ringPos (oldest) to ringPos-1 (newest).
		for i := range ringBufferSize {
			idx := (t.state.ringPos + i) % ringBufferSize
			e := t.state.ring[idx]
			if e != nil && logLevelValue(e["level"]) >= level {
				entries = append(entries, e)
			}
		}
	} else {
		// Buffer not full — read from 0 to ringPos-1.
		for i := 0; i < t.state.ringPos; i++ {
			e := t.state.ring[i]
			if e != nil && logLevelValue(e["level"]) >= level {
				entries = append(entries, e)
			}
		}
	}
	t.state.ringMu.RUnlock()

	for _, e := range entries {
		client.SendEvent(*protocol.NewEvent("log", e))
	}

	// Send sentinel so the client knows tailing started.
	client.SendEvent(*protocol.NewEvent("log", map[string]any{
		"timestamp": time.Now().UnixMilli(),
		"level":     "info",
		"message":   "Log tailing started",
		"source":    "gateway",
	}))
}

// Unsubscribe removes a client from the log tailing set.
func (t *LogTee) Unsubscribe(clientID string) {
	t.state.mu.Lock()
	delete(t.state.clients, clientID)
	t.state.mu.Unlock()
}

func (t *LogTee) AggregateRuntimeLogs(opts runtimelogs.RuntimeAggregateOpts) runtimelogs.RuntimeAggregateResult {
	groupBy := opts.GroupBy
	if groupBy == "" {
		groupBy = "level"
	}
	entries := t.recentEntries()
	counts := map[string]runtimelogs.RuntimeAggregateBucket{}
	sampleSize := 0
	for _, e := range entries {
		if opts.FromMS > 0 && int64Value(e["timestamp"]) < opts.FromMS {
			continue
		}
		level := stringValue(e["level"])
		if opts.Level != "" && level != opts.Level {
			continue
		}
		source := stringValue(e["source"])
		if opts.Source != "" && source != opts.Source {
			continue
		}
		key := level
		if groupBy == "source" {
			key = source
			if key == "" {
				key = "unknown"
			}
		}
		sampleSize++
		b := counts[key]
		b.Key = key
		b.Count++
		ts := int64Value(e["timestamp"])
		if ts > b.LastSeen {
			b.LastSeen = ts
		}
		counts[key] = b
	}
	buckets := make([]runtimelogs.RuntimeAggregateBucket, 0, len(counts))
	for _, b := range counts {
		buckets = append(buckets, b)
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count == buckets[j].Count {
			return buckets[i].LastSeen > buckets[j].LastSeen
		}
		return buckets[i].Count > buckets[j].Count
	})
	return runtimelogs.RuntimeAggregateResult{
		Source:     "runtime",
		Retention:  "ring_buffer",
		Capacity:   ringBufferSize,
		SampleSize: sampleSize,
		GroupBy:    groupBy,
		Buckets:    buckets,
	}
}

func (t *LogTee) recentEntries() []map[string]any {
	t.state.ringMu.RLock()
	defer t.state.ringMu.RUnlock()
	entries := make([]map[string]any, 0, ringBufferSize)
	if t.state.ringFul {
		for i := range ringBufferSize {
			idx := (t.state.ringPos + i) % ringBufferSize
			if e := t.state.ring[idx]; e != nil {
				entries = append(entries, cloneLogEntry(e))
			}
		}
		return entries
	}
	for i := 0; i < t.state.ringPos; i++ {
		if e := t.state.ring[i]; e != nil {
			entries = append(entries, cloneLogEntry(e))
		}
	}
	return entries
}

func cloneLogEntry(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func int64Value(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// logLevelValue converts a level name string back to slog.Level for filtering.
func logLevelValue(v any) slog.Level {
	s, _ := v.(string)
	switch s {
	case "error":
		return slog.LevelError
	case "warn":
		return slog.LevelWarn
	case "info":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

func applyLogAttr(entry, attrs map[string]any, a slog.Attr) {
	key := a.Key
	val := a.Value.String()
	if key == "component" || key == "source" || key == "module" {
		entry["source"] = val
		return
	}
	if isSensitiveKey(key) {
		attrs[key] = redactedValue
		return
	}
	attrs[key] = val
}
