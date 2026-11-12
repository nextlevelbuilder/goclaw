package mattermost

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestStartStop_IsIdempotent(t *testing.T) {
	fs := newFakeServer(t)
	fs.wsHandler = func(conn *websocket.Conn) {
		if !serveAuth(t, conn, "OK") {
			return
		}
		_ = conn.WriteJSON(map[string]any{"event": "hello", "data": map[string]any{}})
		_, _, _ = conn.ReadMessage() // hold until the client disconnects
	}

	ch, err := New(config.MattermostConfig{ServerURL: fs.URL, BotToken: testToken}, bus.New(), nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !ch.IsRunning() || ch.botUserID != testBotID || ch.botUsername != testBotUsername {
		t.Fatalf("running=%v bot=%q/%q", ch.IsRunning(), ch.botUserID, ch.botUsername)
	}

	done := make(chan struct{})
	go func() {
		_ = ch.Stop(context.Background())
		_ = ch.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
	if ch.IsRunning() {
		t.Fatal("channel must not be running after Stop")
	}
}

func TestStart_RejectedTokenFails(t *testing.T) {
	fs := newFakeServer(t)
	ch, err := New(config.MattermostConfig{ServerURL: fs.URL, BotToken: "revoked"}, bus.New(), nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ch.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail with a rejected token")
	}
	if ch.IsRunning() {
		t.Fatal("channel must not run after a failed Start")
	}
	_ = ch.Stop(context.Background()) // must not block without a started flusher
}
