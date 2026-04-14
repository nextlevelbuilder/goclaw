//go:build integration

package ws_methods

import "testing"

// CONTRACT: agents.list response MUST include agents array with required fields.
func TestContract_WS_AgentsList(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client, _ := connect(t, wsURL, nil)

	resp := client.send(t, "agents.list", map[string]any{})

	// Response must have agents array
	assertField(t, resp, "agents", "array")

	// If agents exist, verify each has required fields
	agents, ok := resp["agents"].([]any)
	if !ok || len(agents) == 0 {
		t.Log("No agents returned - skipping field checks")
		return
	}

	agent, ok := agents[0].(map[string]any)
	if !ok {
		t.Error("CONTRACT VIOLATION: agents[0] is not an object")
		return
	}

	// Required agent fields
	assertField(t, agent, "id", "string")
	assertField(t, agent, "agent_key", "string")
	assertField(t, agent, "display_name", "string")
	assertField(t, agent, "status", "string")
	assertField(t, agent, "provider", "string")
	assertField(t, agent, "model", "string")
}
