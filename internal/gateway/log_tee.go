package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// LogTee is a slog.Handler that forwards log records to subscribed WS clients
// while delegating to an underlying handler for normal output.
type LogTee struct {
	inner slog.Handler

	mu      sync.RWMutex
	clients map[string]*Client // client ID → client
}

// NewLogTee wraps an existing slog.Handler so log records are also forwarded
// to any WebSocket clients that have started log tailing.
func NewLogTee(inner slog.Handler) *LogTee {
	return &LogTee{
		inner:   inner,
		clients: make(map[string]*Client),
	}
}

func (t *LogTee) Enabled(ctx context.Context, level slog.Level) bool {
	return t.inner.Enabled(ctx, level)
}

func (t *LogTee) Handle(ctx context.Context, r slog.Record) error {
	// Forward to subscribers (non-blocking).
	t.mu.RLock()
	n := len(t.clients)
	t.mu.RUnlock()

	if n > 0 {
		entry := map[string]interface{}{
			"timestamp": r.Time.UnixMilli(),
			"level":     levelName(r.Level),
			"message":   r.Message,
		}

		// Collect attributes into source hint.
		var src string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "component" || a.Key == "source" || a.Key == "module" {
				src = a.Value.String()
				return false
			}
			return true
		})
		if src != "" {
			entry["source"] = src
		}

		evt := protocol.NewEvent("log", entry)

		t.mu.RLock()
		for _, c := range t.clients {
			c.SendEvent(*evt)
		}
		t.mu.RUnlock()
	}

	return t.inner.Handle(ctx, r)
}

func (t *LogTee) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogTee{
		inner:   t.inner.WithAttrs(attrs),
		clients: t.clients,
		mu:      t.mu,
	}
}

func (t *LogTee) WithGroup(name string) slog.Handler {
	return &LogTee{
		inner:   t.inner.WithGroup(name),
		clients: t.clients,
		mu:      t.mu,
	}
}

// Subscribe adds a client to the log tailing set.
func (t *LogTee) Subscribe(client *Client) {
	t.mu.Lock()
	t.clients[client.ID()] = client
	t.mu.Unlock()

	// Send an initial log entry so the client knows tailing started.
	client.SendEvent(*protocol.NewEvent("log", map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"level":     "info",
		"message":   "Log tailing started",
		"source":    "gateway",
	}))
}

// Unsubscribe removes a client from the log tailing set.
func (t *LogTee) Unsubscribe(clientID string) {
	t.mu.Lock()
	delete(t.clients, clientID)
	t.mu.Unlock()
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
