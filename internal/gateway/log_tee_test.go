package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// --- isSensitiveKey ---

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"key", "api_key", "apiKey", "token", "auth_token",
		"secret", "password", "dsn", "DATABASE_DSN",
		"credential", "authorization", "Authorization",
		"cookie", "session_cookie",
	}
	for _, k := range sensitive {
		if !isSensitiveKey(k) {
			t.Errorf("expected %q to be sensitive", k)
		}
	}

	safe := []string{
		"message", "level", "user_id", "agent_id", "method",
		"timestamp", "error", "status", "duration",
	}
	for _, k := range safe {
		if isSensitiveKey(k) {
			t.Errorf("expected %q to NOT be sensitive", k)
		}
	}
}

// --- LogTee.buildEntry ---

func TestLogTee_BuildEntry_Basic(t *testing.T) {
	lt := NewLogTee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	now := time.Now()
	record := slog.NewRecord(now, slog.LevelInfo, "test message", 0)
	entry := lt.buildEntry(record)

	if entry["level"] != "info" {
		t.Errorf("level = %v, want %q", entry["level"], "info")
	}
	if entry["message"] != "test message" {
		t.Errorf("message = %v, want %q", entry["message"], "test message")
	}
	if entry["timestamp"] != now.UnixMilli() {
		t.Errorf("timestamp = %v, want %d", entry["timestamp"], now.UnixMilli())
	}
}

func TestLogTee_BuildEntry_RedactsSensitiveAttrs(t *testing.T) {
	lt := NewLogTee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "connecting", 0)
	record.AddAttrs(
		slog.String("api_key", "sk-secret-123"),
		slog.String("user_id", "alice"),
		slog.String("token", "bearer-abc"),
	)

	entry := lt.buildEntry(record)
	attrs := entry["attrs"].(map[string]any)
	if attrs["api_key"] != redactedValue {
		t.Errorf("api_key should be redacted, got %v", attrs["api_key"])
	}
	if attrs["token"] != redactedValue {
		t.Errorf("token should be redacted, got %v", attrs["token"])
	}
	if attrs["user_id"] == redactedValue {
		t.Error("user_id should NOT be redacted")
	}
}

func TestLogTee_BuildEntry_ExtractsSource(t *testing.T) {
	lt := NewLogTee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "starting", 0)
	record.AddAttrs(slog.String("component", "agent"))

	entry := lt.buildEntry(record)
	if entry["source"] != "agent" {
		t.Errorf("source = %v, want %q", entry["source"], "agent")
	}
	if attrs, ok := entry["attrs"].(map[string]any); ok {
		if _, found := attrs["component"]; found {
			t.Error("component should be extracted to source, not in attrs")
		}
	}
}

// --- LogTee.Enabled ---

func TestLogTee_Enabled_DelegatesToInner(t *testing.T) {
	lt := NewLogTee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))

	if !lt.Enabled(context.Background(), slog.LevelError) {
		t.Error("should be enabled for error (inner accepts warn+)")
	}
	if lt.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("should NOT be enabled for info (inner only accepts warn+)")
	}
}

// --- LogTee.Handle delivers to subscribers ---

func TestLogTee_Handle_DeliversToSubscriber(t *testing.T) {
	lt := NewLogTee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c := &Client{id: "sub-client", send: make(chan []byte, 256)}
	lt.Subscribe(c, slog.LevelInfo)

	// Handle a log record — subscriber should receive it as an event.
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hello from log", 0)
	lt.Handle(context.Background(), record)

	// Drain subscribe replay (sentinel "Log tailing started" + any ring replay)
	// then check for our "hello from log" message.
	deadline := time.After(500 * time.Millisecond)
	found := false
	for !found {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for log event delivery to subscriber")
		case msg := <-c.send:
			if string(msg) == "" {
				continue
			}
			// Check if the delivered event contains our message.
			if containsSubstring(msg, "hello from log") {
				found = true
			}
		}
	}
}

// --- LogTee.Unsubscribe stops delivery ---

func TestLogTee_Unsubscribe_StopsDelivery(t *testing.T) {
	lt := NewLogTee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c := &Client{id: "unsub-client", send: make(chan []byte, 256)}
	lt.Subscribe(c, slog.LevelInfo)

	// Drain the subscribe sentinel
	drainChannel(c.send, 100*time.Millisecond)

	lt.Unsubscribe("unsub-client")

	// Handle a record after unsubscribe — should NOT be delivered.
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "after unsub", 0)
	lt.Handle(context.Background(), record)

	time.Sleep(50 * time.Millisecond)
	select {
	case msg := <-c.send:
		if containsSubstring(msg, "after unsub") {
			t.Error("should NOT receive events after Unsubscribe")
		}
	default:
		// Good — no message delivered.
	}
}

func containsSubstring(data []byte, sub string) bool {
	return len(data) > 0 && len(sub) > 0 && bytesContains(data, []byte(sub))
}

func bytesContains(haystack, needle []byte) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func drainChannel(ch <-chan []byte, timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		select {
		case <-ch:
		case <-deadline:
			return
		}
	}
}
