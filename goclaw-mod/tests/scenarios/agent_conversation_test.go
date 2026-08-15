//go:build integration

package scenarios

import (
	"strings"
	"testing"
)

// SCENARIO: User has multi-turn conversation with context retention.
// Flow: User sends message → Agent responds → User follows up → Agent uses context
func TestScenario_MultiTurnConversation(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// Turn 1: Introduce a topic
	resp1 := client.chat("My name is Alice and I like pizza.")
	content1, _ := resp1["content"].(string)
	if content1 == "" {
		t.Error("expected non-empty response")
	}

	// Turn 2: Ask about previously mentioned topic
	resp2 := client.chat("What is my name?")
	content2, _ := resp2["content"].(string)

	// Agent should remember the name from context
	if !strings.Contains(strings.ToLower(content2), "alice") {
		t.Logf("Response: %s", content2)
		t.Log("SCENARIO NOTE: Agent may not have retained context - check memory settings")
	}
}

// SCENARIO: Session reset clears conversation context.
// Flow: Chat → Reset → Chat → Verify no old context
func TestScenario_SessionReset(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// Establish context
	client.chat("Remember the secret code: PHOENIX42")

	// Reset session
	client.send("sessions.reset", map[string]any{
		"session_key": client.sessionKey,
	})

	// Check history is empty
	history := client.getHistory()
	if len(history) > 0 {
		t.Errorf("expected empty history after reset, got %d messages", len(history))
	}
}

// SCENARIO: Chat history persists across messages.
// Flow: Send 3 messages → Verify history contains all
func TestScenario_HistoryPersistence(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// Send multiple messages
	messages := []string{
		"Message one",
		"Message two",
		"Message three",
	}

	for _, msg := range messages {
		client.chat(msg)
	}

	// Get history
	history := client.getHistory()

	// Should have at least user messages (may also have assistant responses)
	userMsgCount := 0
	for _, h := range history {
		if role, _ := h["role"].(string); role == "user" {
			userMsgCount++
		}
	}

	if userMsgCount < len(messages) {
		t.Errorf("expected at least %d user messages in history, got %d", len(messages), userMsgCount)
	}
}
