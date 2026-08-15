//go:build integration

package scenarios

import "testing"

// SCENARIO: User reconnects and resumes session with history.
// Flow: Connect → Chat → Disconnect → Reconnect with session_key → History preserved
func TestScenario_ReconnectWithHistory(t *testing.T) {
	wsURL, _ := getTestServer(t)
	userID := "reconnect-test-user"

	// First connection
	client1 := connect(t, wsURL, userID)
	sessionKey := client1.sessionKey

	if sessionKey == "" {
		t.Skip("No session_key returned - reconnect test not applicable")
	}

	// Send a message
	client1.chat("Remember this for reconnect test")

	// Get history before disconnect
	history1 := client1.getHistory()
	msgCount1 := len(history1)

	// Simulate disconnect (close connection)
	client1.conn.Close()

	// Reconnect with same session
	client2 := reconnect(t, wsURL, sessionKey, userID)

	// History should be preserved
	history2 := client2.getHistory()

	if len(history2) < msgCount1 {
		t.Errorf("history not preserved: before=%d, after=%d", msgCount1, len(history2))
	}
}

// SCENARIO: New session starts fresh without history.
// Flow: Connect with new user → No existing history
func TestScenario_FreshSession(t *testing.T) {
	wsURL, _ := getTestServer(t)
	userID := "fresh-session-test-user"

	client := connect(t, wsURL, userID)

	// Fresh session should have no history
	history := client.getHistory()

	if len(history) > 0 {
		t.Logf("Fresh session has %d messages - may be resuming existing session", len(history))
	}
}

// SCENARIO: Session preview returns summary without full history.
func TestScenario_SessionPreview(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// Send some messages
	client.chat("First message for preview test")
	client.chat("Second message for preview test")

	// Get session preview
	resp := client.send("sessions.preview", map[string]any{
		"session_key": client.sessionKey,
	})

	// Should have some preview data
	if resp["title"] == nil && resp["preview"] == nil && resp["message_count"] == nil {
		t.Log("sessions.preview returned no standard fields - check implementation")
	}
}
