//go:build integration

// Package ws_methods tests WebSocket RPC method contracts.
// Contract tests verify response schemas don't change without version bump.
package ws_methods

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

var testToken string

// getTestServer returns WS URL and token from environment.
// Set CONTRACT_TEST_WS_URL and CONTRACT_TEST_TOKEN to run against a real server.
func getTestServer(t *testing.T) (wsURL string, token string) {
	t.Helper()

	wsURL = os.Getenv("CONTRACT_TEST_WS_URL")
	token = os.Getenv("CONTRACT_TEST_TOKEN")

	if wsURL == "" {
		t.Skip("CONTRACT_TEST_WS_URL not set - skipping contract test")
	}
	if token == "" {
		t.Skip("CONTRACT_TEST_TOKEN not set - skipping contract test")
	}

	testToken = token
	return wsURL, token
}

// wsClient wraps a WebSocket connection for contract testing.
type wsClient struct {
	conn   *websocket.Conn
	nextID int
}

// connect creates a WS client and performs connect handshake.
func connect(t *testing.T, wsURL string, params map[string]any) (*wsClient, map[string]any) {
	t.Helper()

	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["token"]; !ok {
		params["token"] = testToken
	}
	if _, ok := params["user_id"]; !ok {
		params["user_id"] = "contract-test-user"
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	c := &wsClient{conn: conn, nextID: 1}
	t.Cleanup(func() { conn.Close() })

	resp := c.send(t, protocol.MethodConnect, params)
	return c, resp
}

// send sends a request and waits for response.
func (c *wsClient) send(t *testing.T, method string, params any) map[string]any {
	t.Helper()

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
		t.Fatalf("send %s: %v", method, err)
	}

	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var resp protocol.ResponseFrame
	if err := c.conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}

	if resp.ID != reqID {
		t.Fatalf("response ID mismatch: got %s, want %s", resp.ID, reqID)
	}
	if !resp.OK {
		t.Fatalf("%s failed: %s - %s", method, resp.Error.Code, resp.Error.Message)
	}

	payloadJSON, _ := json.Marshal(resp.Payload)
	var result map[string]any
	json.Unmarshal(payloadJSON, &result)
	return result
}

// assertField verifies a field exists with expected type.
func assertField(t *testing.T, data map[string]any, path string, expectedType string) {
	t.Helper()

	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			val, ok := current[part]
			if !ok {
				t.Errorf("CONTRACT VIOLATION: missing field %q", path)
				return
			}
			actualType := typeOf(val)
			if actualType != expectedType {
				t.Errorf("CONTRACT VIOLATION: field %q type mismatch: got %s, want %s", path, actualType, expectedType)
			}
			return
		}
		nested, ok := current[part].(map[string]any)
		if !ok {
			t.Errorf("CONTRACT VIOLATION: field %q is not an object", strings.Join(parts[:i+1], "."))
			return
		}
		current = nested
	}
}

// assertFieldValue verifies a field has the expected value.
func assertFieldValue(t *testing.T, data map[string]any, path string, expected any) {
	t.Helper()

	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			val, ok := current[part]
			if !ok {
				t.Errorf("CONTRACT VIOLATION: missing field %q", path)
				return
			}
			if val != expected {
				t.Errorf("CONTRACT VIOLATION: field %q value mismatch: got %v, want %v", path, val, expected)
			}
			return
		}
		nested, ok := current[part].(map[string]any)
		if !ok {
			t.Errorf("CONTRACT VIOLATION: field %q is not an object", strings.Join(parts[:i+1], "."))
			return
		}
		current = nested
	}
}

func typeOf(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
