package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Client represents a single WebSocket connection.
type Client struct {
	id            string
	conn          *websocket.Conn
	server        *Server
	authenticated bool
	role          permissions.Role
	userID        string // external user ID (TEXT, free-form), set during connect
	send          chan []byte

	connectedAt time.Time // when the client connected
	remoteAddr  string    // peer IP (extracted from proxy headers or RemoteAddr)

	// Browser pairing state
	pairingCode     string // 8-char code if pending approval
	pairingPending  bool   // true while waiting for admin approval
	pairedSenderID  string // senderID used for browser pairing auth (for revocation lookup)
	pairedChannel   string // channel used for pairing auth (e.g., "browser")
}

func NewClient(conn *websocket.Conn, server *Server, remoteIP string) *Client {
	return &Client{
		id:          uuid.NewString(),
		conn:        conn,
		server:      server,
		send:        make(chan []byte, 256),
		connectedAt: time.Now(),
		remoteAddr:  remoteIP,
	}
}

// Run starts the read and write pumps for this client.
func (c *Client) Run(ctx context.Context) {
	go c.writePump()
	c.readPump(ctx)
}

// maxWSMessageSize is the maximum allowed WebSocket message size (512KB).
// Gorilla/websocket closes the connection with ErrReadLimit if exceeded.
const maxWSMessageSize = 512 * 1024

// readPump reads frames from the WebSocket connection.
func (c *Client) readPump(ctx context.Context) {
	defer c.conn.Close()

	c.conn.SetReadLimit(maxWSMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("websocket read error", "client", c.id, "error", err)
			}
			return
		}

		// Reset read deadline on activity
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		c.handleFrame(ctx, data)
	}
}

// writePump writes frames and pings to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleFrame parses and dispatches a single frame.
func (c *Client) handleFrame(ctx context.Context, data []byte) {
	frameType, err := protocol.ParseFrameType(data)
	if err != nil {
		c.sendError("", protocol.ErrInvalidRequest, "invalid frame: "+err.Error())
		return
	}

	switch frameType {
	case protocol.FrameTypeRequest:
		var req protocol.RequestFrame
		if err := json.Unmarshal(data, &req); err != nil {
			c.sendError("", protocol.ErrInvalidRequest, "malformed request: "+err.Error())
			return
		}

		// First request must be "connect" (except browser.pairing.status for pending clients)
		if !c.authenticated && req.Method != protocol.MethodConnect {
			if !(c.pairingPending && req.Method == protocol.MethodBrowserPairingStatus) {
				c.sendError(req.ID, protocol.ErrUnauthorized, "first request must be 'connect'")
				return
			}
		}

		// Dispatch to method router
		c.server.router.Handle(ctx, c, &req)

	default:
		c.sendError("", protocol.ErrInvalidRequest, "unexpected frame type: "+frameType)
	}
}

// SendResponse sends a response frame to this client.
func (c *Client) SendResponse(resp *protocol.ResponseFrame) {
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("marshal response failed", "error", err)
		return
	}
	select {
	case c.send <- data:
	default:
		slog.Warn("client send buffer full, dropping message", "client", c.id)
	}
}

// SendEvent sends an event frame to this client.
func (c *Client) SendEvent(event protocol.EventFrame) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("marshal event failed", "error", err)
		return
	}
	select {
	case c.send <- data:
	default:
		slog.Warn("client send buffer full, dropping event", "client", c.id)
	}
}

func (c *Client) sendError(id, code, message string) {
	c.SendResponse(protocol.NewErrorResponse(id, code, message))
}

// ID returns the client's unique identifier.
func (c *Client) ID() string { return c.id }

// Role returns the client's permission role.
func (c *Client) Role() permissions.Role { return c.role }

// UserID returns the external user ID set during connect.
func (c *Client) UserID() string { return c.userID }

// ConnectedAt returns when the client connected.
func (c *Client) ConnectedAt() time.Time { return c.connectedAt }

// RemoteAddr returns the peer IP:port.
func (c *Client) RemoteAddr() string { return c.remoteAddr }

// Close shuts down the client connection.
func (c *Client) Close() {
	close(c.send)
}
