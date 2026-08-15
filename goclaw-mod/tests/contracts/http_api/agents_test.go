//go:build integration

package http_api

import "testing"

// CONTRACT: /v1/agents response MUST include agents array.
func TestContract_HTTP_AgentsList(t *testing.T) {
	baseURL, token := getTestServer(t)
	client := newHTTPClient(baseURL, token)

	resp := client.get(t, "/v1/agents")

	assertField(t, resp, "agents", "array")

	agents, ok := resp["agents"].([]any)
	if !ok || len(agents) == 0 {
		t.Log("No agents - skipping field checks")
		return
	}

	agent := agents[0].(map[string]any)
	assertField(t, agent, "id", "string")
	assertField(t, agent, "agent_key", "string")
}
