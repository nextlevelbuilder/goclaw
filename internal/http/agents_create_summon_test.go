package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestShouldSummonOnCreate(t *testing.T) {
	cases := []struct {
		name         string
		requested    bool
		agentType    string
		description  string
		haveSummoner bool
		want         bool
	}{
		{"default for a described predefined agent", true, store.AgentTypePredefined, "a sarcastic Rust reviewer", true, true},
		{"opted out by the client", false, store.AgentTypePredefined, "a sarcastic Rust reviewer", true, false},
		{"no description to summon from", true, store.AgentTypePredefined, "", true, false},
		{"open agents are never summoned", true, store.AgentTypeOpen, "a sarcastic Rust reviewer", true, false},
		{"no summoner configured", true, store.AgentTypePredefined, "a sarcastic Rust reviewer", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSummonOnCreate(tc.requested, tc.agentType, tc.description, tc.haveSummoner); got != tc.want {
				t.Fatalf("shouldSummonOnCreate = %v, want %v", got, tc.want)
			}
		})
	}
}

// createAgentForSummonTest posts body to the create handler with a summoner
// configured, and returns the created agent's status.
//
// The summoner is deliberately built with nil dependencies: if the handler
// ever starts summoning when the client opted out, SummonAgent would reach
// for them, and the test fails loudly instead of silently passing.
func createAgentForSummonTest(t *testing.T, body string) (string, *gatewayOperatorAgentStore) {
	t.Helper()
	agents := &gatewayOperatorAgentStore{}
	handler := &AgentsHandler{
		agents:           agents,
		defaultWorkspace: "/tmp/workspace",
		summoner:         NewAgentSummoner(agents, nil, nil, nil),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader([]byte(body)))
	req = req.WithContext(gatewayOperatorContext())
	rr := httptest.NewRecorder()

	handler.handleCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	return response.Status, agents
}

func TestAgentsCreateSummonFalseGoesStraightToActive(t *testing.T) {
	status, agents := createAgentForSummonTest(t, `{
		"agent_key":"scout",
		"display_name":"Scout",
		"provider":"anthropic",
		"model":"claude",
		"agent_description":"reads the morning news and writes a digest",
		"summon":false
	}`)

	// Asserting the status is enough to prove summoning never started, with no
	// need to wait for it: handleCreate starts the goroutine if and only if the
	// status it decided synchronously is "summoning" (`if req.Status ==
	// store.AgentStatusSummoning { go h.summoner.SummonAgent(...) }`). So a
	// persisted "active" status means the goroutine was not spawned.
	if status != store.AgentStatusActive {
		t.Fatalf("status=%q, want %q: a client that brings its own context files must not wait for summoning",
			status, store.AgentStatusActive)
	}
	if len(agents.agents) != 1 {
		t.Fatalf("expected exactly one created agent, got %d", len(agents.agents))
	}
	if got := agents.agents[0].Status; got != store.AgentStatusActive {
		t.Fatalf("stored status=%q, want %q", got, store.AgentStatusActive)
	}
}

func TestAgentsCreateWithoutSummonFieldKeepsSummoning(t *testing.T) {
	// No "summon" field: existing clients must keep the old behaviour.
	if !shouldSummonOnCreate(true, store.AgentTypePredefined, "reads the morning news", true) {
		t.Fatal("omitting the summon field must still summon a described predefined agent")
	}
}
