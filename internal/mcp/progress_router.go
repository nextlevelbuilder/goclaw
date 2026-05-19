package mcp

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type mcpProgressEvent struct {
	ServerName string
	Token      string
	Progress   float64
	Total      float64
	Message    string
	Event      string
	RunID      string
	Timestamp  string
	EventData  map[string]any
}

type mcpProgressRouter struct {
	mu        sync.RWMutex
	callbacks map[string]func(mcpProgressEvent)
}

func newMCPProgressRouter() *mcpProgressRouter {
	return &mcpProgressRouter{callbacks: make(map[string]func(mcpProgressEvent))}
}

func (r *mcpProgressRouter) register(token string, cb func(mcpProgressEvent)) func() {
	if r == nil || token == "" || cb == nil {
		return func() {}
	}
	r.mu.Lock()
	r.callbacks[token] = cb
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.callbacks, token)
		r.mu.Unlock()
	}
}

func (r *mcpProgressRouter) handle(serverName string, notification mcpgo.JSONRPCNotification) {
	if r == nil || notification.Method != "notifications/progress" {
		return
	}

	fields := notification.Params.AdditionalFields
	token := fmt.Sprint(fields["progressToken"])
	if token == "<nil>" {
		token = ""
	}
	ev := mcpProgressEvent{
		ServerName: serverName,
		Token:      token,
		Progress:   numberField(fields["progress"]),
		Total:      numberField(fields["total"]),
		Message:    stringField(fields["message"]),
		Event:      stringField(fields["event"]),
		RunID:      stringField(fields["run_id"]),
		Timestamp:  stringField(fields["timestamp"]),
		EventData:  mapField(fields["event_data"]),
	}

	slog.Info("mcp.progress.received",
		"server", serverName,
		"token", token,
		"progress", ev.Progress,
		"total", ev.Total,
		"message", ev.Message,
		"event", ev.Event,
		"run_id", ev.RunID,
	)

	r.mu.RLock()
	cb := r.callbacks[token]
	r.mu.RUnlock()
	if cb == nil {
		slog.Debug("mcp.progress.unmatched", "server", serverName, "token", token)
		return
	}
	cb(ev)
}

func numberField(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	case jsonNumber:
		f, _ := strconv.ParseFloat(n.String(), 64)
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

type jsonNumber interface {
	String() string
}

func stringField(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func mapField(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
