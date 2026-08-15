//go:build integration

// Package scenarios tests end-to-end user journeys.
// Scenario tests verify complete workflows, not individual API contracts.
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

var (
	testWSURL  string
	testToken  string
	testUserID string
)

// getTestServer returns WS URL and token from environment.
func getTestServer(t *testing.T) (wsURL string, token string) {
	t.Helper()

	wsURL = os.Getenv("SCENARIO_TEST_WS_URL")
	if wsURL == "" {
		wsURL = os.Getenv("CONTRACT_TEST_WS_URL") // fallback
	}
	token = os.Getenv("SCENARIO_TEST_TOKEN")
	if token == "" {
		token = os.Getenv("CONTRACT_TEST_TOKEN")
	}

	if wsURL == "" {
		t.Skip("SCENARIO_TEST_WS_URL not set - skipping scenario test")
	}
	if token == "" {
		t.Skip("SCENARIO_TEST_TOKEN not set - skipping scenario test")
	}

	testWSURL = wsURL
	testToken = token
	testUserID = fmt.Sprintf("scenario-user-%d", time.Now().UnixNano())
	return wsURL, token
}

// scenarioClient wraps a WebSocket connection for scenario testing.
type scenarioClient struct {
	conn      *websocket.Conn
	nextID    int
	sessionKey string
	t         *testing.T
}

// connect creates a new scenario client with fresh session.
func connect(t *testing.T, wsURL string, userID string) *scenarioClient {
	t.Helper()

	if userID == "" {
		userID = testUserID
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	c := &scenarioClient{conn: conn, nextID: 1, t: t}
	t.Cleanup(func() { conn.Close() })

	// Connect handshake
	resp := c.send("connect", map[string]any{
		"token":   testToken,
		"user_id": userID,
	})

	if sk, ok := resp["session_key"].(string); ok {
		c.sessionKey = sk
	}

	return c
}

// reconnect creates a client that resumes an existing session.
func reconnect(t *testing.T, wsURL string, sessionKey string, userID string) *scenarioClient {
	t.Helper()

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	c := &scenarioClient{conn: conn, nextID: 1, sessionKey: sessionKey, t: t}
	t.Cleanup(func() { conn.Close() })

	// Connect with existing session
	c.send("connect", map[string]any{
		"token":       testToken,
		"user_id":     userID,
		"session_key": sessionKey,
	})

	return c
}

// send sends a request and waits for response.
func (c *scenarioClient) send(method string, params any) map[string]any {
	c.t.Helper()

	reqID := fmt.Sprintf("req-%d", c.nextID)
	c.nextID++

	paramsJSON, _ := json.Marshal(params)
	req := protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     reqID,
		Method: method,
		Params: paramsJSON,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		c.t.Fatalf("send %s: %v", method, err)
	}

	c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var resp protocol.ResponseFrame
	if err := c.conn.ReadJSON(&resp); err != nil {
		c.t.Fatalf("read %s response: %v", method, err)
	}

	if resp.ID != reqID {
		c.t.Fatalf("response ID mismatch: got %s, want %s", resp.ID, reqID)
	}
	if !resp.OK {
		c.t.Fatalf("%s failed: %s - %s", method, resp.Error.Code, resp.Error.Message)
	}

	payloadJSON, _ := json.Marshal(resp.Payload)
	var result map[string]any
	json.Unmarshal(payloadJSON, &result)
	return result
}

// sendExpectError sends a request expecting an error response.
func (c *scenarioClient) sendExpectError(method string, params any) (code string, message string) {
	c.t.Helper()

	reqID := fmt.Sprintf("req-%d", c.nextID)
	c.nextID++

	paramsJSON, _ := json.Marshal(params)
	req := protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     reqID,
		Method: method,
		Params: paramsJSON,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		c.t.Fatalf("send %s: %v", method, err)
	}

	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var resp protocol.ResponseFrame
	if err := c.conn.ReadJSON(&resp); err != nil {
		c.t.Fatalf("read %s response: %v", method, err)
	}

	if resp.OK {
		c.t.Fatalf("expected error but got success for %s", method)
	}

	return resp.Error.Code, resp.Error.Message
}

// chat sends a chat message and returns the response.
func (c *scenarioClient) chat(message string) map[string]any {
	return c.send("chat.send", map[string]any{
		"message": message,
	})
}

// getHistory retrieves chat history for current session.
func (c *scenarioClient) getHistory() []map[string]any {
	resp := c.send("chat.history", map[string]any{})
	messages, _ := resp["messages"].([]any)
	result := make([]map[string]any, len(messages))
	for i, m := range messages {
		result[i], _ = m.(map[string]any)
	}
	return result
}
