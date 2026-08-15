package protocol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// WSClient wraps gorilla/websocket with a thread-safe write method.
type WSClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// DialWS connects to a WebSocket endpoint using gorilla/websocket.
// Manually injects cookies from the jar using https:// scheme to bypass
// Go's cookiejar domain-matching limitations with wss:// URLs and host-only cookies.
func DialWS(ctx context.Context, wsURL string, headers http.Header, jar http.CookieJar) (*WSClient, error) {
	dialer := websocket.Dialer{
		EnableCompression: true,
	}
	// Don't pass jar to gorilla — we inject cookies manually below.
	// This avoids issues with host-only cookies not matching WS subdomains.

	// Inject cookies from chat.zalo.me base domain + the WS host itself.
	if jar != nil {
		injectCookies(headers, jar, wsURL)
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: ws dial: %w", err)
	}
	conn.SetReadLimit(1 << 20) // 1MB
	return &WSClient{conn: conn}, nil
}

// injectCookies merges cookies from the base domain (chat.zalo.me) and the
// WS host into the request headers. Deduplicates by name so the most specific
// (WS host) cookie wins.
func injectCookies(headers http.Header, jar http.CookieJar, wsURL string) {
	baseURL := &url.URL{Scheme: "https", Host: "chat.zalo.me", Path: "/"}

	// Collect from base domain first, then WS host (WS host overrides).
	seen := make(map[string]string, 4)
	for _, c := range jar.Cookies(baseURL) {
		seen[c.Name] = c.Name + "=" + c.Value
	}
	// Also try the exact WS host with https:// scheme
	if u, err := url.Parse(strings.Replace(wsURL, "wss://", "https://", 1)); err == nil {
		for _, c := range jar.Cookies(u) {
			seen[c.Name] = c.Name + "=" + c.Value
		}
	}

	if len(seen) == 0 {
		return
	}

	parts := make([]string, 0, len(seen))
	for _, v := range seen {
		parts = append(parts, v)
	}
	cookieHeader := strings.Join(parts, "; ")

	// Merge with any existing Cookie header
	if existing := headers.Get("Cookie"); existing != "" {
		cookieHeader = existing + "; " + cookieHeader
	}
	headers.Set("Cookie", cookieHeader)

	slog.Info("zalo ws cookies injected", "count", len(seen), "url", wsURL)
}

// ReadMessage reads the next WebSocket message. Blocks until a message arrives
// or the connection is closed. NOTE: gorilla/websocket ReadMessage is blocking
// and does not observe ctx. Cancellation works indirectly: Stop() closes the
// connection which unblocks ReadMessage with an error. The listener applies
// read deadlines via conn.SetReadDeadline for silent disconnect detection.
func (c *WSClient) ReadMessage(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.ReadMessage()
	return data, err
}

// WriteMessage sends a binary WebSocket message. Thread-safe.
func (c *WSClient) WriteMessage(ctx context.Context, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

// Close sends a close frame and shuts down the connection.
func (c *WSClient) Close(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
	)
	c.conn.Close()
}

// parseWSCloseInfo extracts close code and reason from a gorilla/websocket error.
func parseWSCloseInfo(err error) CloseInfo {
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
		return CloseInfo{Code: ce.Code, Reason: ce.Text}
	}
	return CloseInfo{Code: 1006, Reason: err.Error()}
}
