//go:build integration

package ws_methods

import "testing"

// CONTRACT: chat.send response MUST include message fields.
func TestContract_WS_ChatSend(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client, _ := connect(t, wsURL, nil)

	resp := client.send(t, "chat.send", map[string]any{
		"message": "Hello",
	})

	// Required fields for chat response
	assertField(t, resp, "message_id", "string")
	assertField(t, resp, "content", "string")
	assertField(t, resp, "role", "string")
}

// CONTRACT: sessions.list response MUST include sessions array.
func TestContract_WS_SessionsList(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client, _ := connect(t, wsURL, nil)

	resp := client.send(t, "sessions.list", map[string]any{})

	assertField(t, resp, "sessions", "array")
}
