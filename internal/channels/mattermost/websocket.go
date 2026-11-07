package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

const (
	wsHandshakeTimeout = 15 * time.Second
	wsPingInterval     = 30 * time.Second
	wsReadTimeout      = 75 * time.Second // > 2 ping intervals: one lost pong is tolerated
	wsWriteTimeout     = 10 * time.Second
	wsReadLimit        = 4 << 20
	wsInitialBackoff   = 1 * time.Second
	wsMaxBackoff       = 60 * time.Second
	wsAuthSeq          = 1
)

// wsEvent is one frame from the Mattermost websocket. Server events carry
// Event/Data/Broadcast; replies to client actions carry Status/SeqReply.
type wsEvent struct {
	Event     string         `json:"event,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Broadcast struct {
		ChannelID string `json:"channel_id"`
		UserID    string `json:"user_id"`
		TeamID    string `json:"team_id"`
	} `json:"broadcast"`
	Status   string         `json:"status,omitempty"`
	SeqReply int64          `json:"seq_reply,omitempty"`
	Error    map[string]any `json:"error,omitempty"`
}

// dataString returns a string field from the event data map ("" if absent).
func (e *wsEvent) dataString(key string) string {
	if v, ok := e.Data[key].(string); ok {
		return v
	}
	return ""
}

// runWebSocket keeps the websocket connected until ctx is cancelled or the
// server rejects the token. Reconnects with capped exponential backoff.
func (c *Channel) runWebSocket(ctx context.Context) {
	backoff := wsInitialBackoff
	for {
		authenticated, err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			return
		}
		// A rejected authentication_challenge may end in a plain close with no
		// FAIL reply, which looks like a network drop. When the session never
		// got through, ask the REST API whether the token is still valid.
		if !authenticated && !isAuthError(err) {
			if _, meErr := c.api.getMe(ctx); isAuthError(meErr) {
				err = meErr
			}
		}
		if isAuthError(err) {
			slog.Error("mattermost: token rejected, stopping channel", "channel", c.Name(), "error", err)
			c.MarkFailed("Token rejected", err.Error(), channels.ChannelFailureKindAuth, false)
			return
		}
		if authenticated {
			backoff = wsInitialBackoff
		}
		slog.Warn("mattermost: websocket disconnected, reconnecting",
			"channel", c.Name(), "error", err, "retry_in", backoff)
		c.MarkDegraded("Reconnecting", fmt.Sprint(err), channels.ChannelFailureKindNetwork, true)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, wsMaxBackoff)
	}
}

// connectAndServe runs one websocket session. Returns whether the session got
// past authentication (used to reset backoff) and the reason it ended.
func (c *Channel) connectAndServe(ctx context.Context) (bool, error) {
	dialer := websocket.Dialer{
		NetDialContext:   newDialer(wsHandshakeTimeout).DialContext,
		HandshakeTimeout: wsHandshakeTimeout,
	}
	conn, resp, err := dialer.DialContext(ctx, c.api.websocketURL(), nil)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return false, &apiError{StatusCode: resp.StatusCode, Message: "websocket handshake rejected"}
		}
		return false, fmt.Errorf("dial websocket: %w", err)
	}
	defer conn.Close()

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Unblock ReadJSON when the channel stops.
	go func() {
		<-sessionCtx.Done()
		_ = conn.Close()
	}()

	conn.SetReadLimit(wsReadLimit)
	extendDeadline := func() { _ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout)) }
	extendDeadline()
	conn.SetPongHandler(func(string) error {
		extendDeadline()
		return nil
	})
	conn.SetPingHandler(func(data string) error {
		extendDeadline()
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(wsWriteTimeout))
	})

	// The only data frame we ever write; it goes out before the ping
	// goroutine starts, so there is never a concurrent data writer.
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	if err := conn.WriteJSON(map[string]any{
		"seq":    wsAuthSeq,
		"action": "authentication_challenge",
		"data":   map[string]string{"token": c.api.token},
	}); err != nil {
		return false, fmt.Errorf("send authentication challenge: %w", err)
	}

	go func() {
		defer safego.Recover(nil, "component", "mattermost_ws_ping")
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				// WriteControl is safe to call concurrently with readers.
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(wsWriteTimeout)); err != nil {
					return
				}
			}
		}
	}()

	authenticated := false
	for {
		var ev wsEvent
		if err := conn.ReadJSON(&ev); err != nil {
			return authenticated, fmt.Errorf("read websocket: %w", err)
		}
		extendDeadline()

		if ev.SeqReply == wsAuthSeq && ev.Event == "" {
			if ev.Status != "OK" {
				return false, &apiError{StatusCode: http.StatusUnauthorized, Message: "websocket authentication failed"}
			}
			continue
		}

		switch ev.Event {
		case "hello":
			authenticated = true
			c.MarkHealthy("Connected")
			slog.Info("mattermost: websocket connected", "channel", c.Name())
		case "posted", "post_edited":
			select {
			case c.events <- ev:
			case <-sessionCtx.Done():
				return authenticated, sessionCtx.Err()
			}
		}
	}
}

// eventWorker handles post events sequentially so per-channel message order
// is preserved while the read loop keeps answering pings.
func (c *Channel) eventWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-c.events:
			switch ev.Event {
			case "posted":
				c.handlePostedEvent(ctx, ev)
			case "post_edited":
				c.handlePostEditedEvent(ctx, ev)
			}
		}
	}
}

// parsePostedEvent decodes the post JSON embedded in a "posted" or
// "post_edited" event.
func parsePostedEvent(ev wsEvent) (*mmPost, error) {
	raw := ev.dataString("post")
	if raw == "" {
		return nil, fmt.Errorf("%s event without post", ev.Event)
	}
	var post mmPost
	if err := json.Unmarshal([]byte(raw), &post); err != nil {
		return nil, fmt.Errorf("decode post: %w", err)
	}
	return &post, nil
}
