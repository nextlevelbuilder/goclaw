package sessions

import (
	"strings"
	"testing"
)

// TestBuildHeartbeatSessionKey_UsesAgentKey verifies that heartbeat session keys
// use agentKey (human-readable, e.g. "my-agent"), NOT the agent's UUID.
// This is critical: the agent router cache is invalidated by agentKey suffix match,
// so UUID-based keys would never be invalidated on agent config updates.
func TestBuildHeartbeatSessionKey_UsesAgentKey(t *testing.T) {
	agentKey := "my-agent"
	key := BuildHeartbeatSessionKey(agentKey, false)

	if !strings.HasPrefix(key, "agent:my-agent:") {
		t.Errorf("expected session key to use agentKey, got %q", key)
	}
	if key != "agent:my-agent:heartbeat" {
		t.Errorf("expected agent:my-agent:heartbeat, got %q", key)
	}
}

// TestBuildHeartbeatSessionKey_Isolated verifies isolated heartbeat sessions
// include a timestamp suffix.
func TestBuildHeartbeatSessionKey_Isolated(t *testing.T) {
	key := BuildHeartbeatSessionKey("my-agent", true)

	if !strings.HasPrefix(key, "agent:my-agent:heartbeat:") {
		t.Errorf("expected agent:my-agent:heartbeat:{timestamp}, got %q", key)
	}
}

// TestBuildCronSessionKey_UsesAgentKey verifies that cron session keys use agentKey.
// CronJob.AgentID is a UUID from DB — callers must resolve it to agentKey before calling.
func TestBuildCronSessionKey_UsesAgentKey(t *testing.T) {
	agentKey := "my-agent"
	key := BuildCronSessionKey(agentKey, "job-123")

	if key != "agent:my-agent:cron:job-123" {
		t.Errorf("expected agent:my-agent:cron:job-123, got %q", key)
	}
}

// TestBuildSessionKey_UUIDIsWrong documents that passing a UUID instead of agentKey
// produces a session key that won't match cache invalidation patterns.
// This test exists as a guard: if someone passes a UUID, the key will contain
// it literally, which breaks InvalidateAgent(agentKey) suffix matching.
func TestBuildSessionKey_UUIDIsWrong(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	agentKey := "my-agent"

	uuidKey := BuildHeartbeatSessionKey(uuid, false)
	correctKey := BuildHeartbeatSessionKey(agentKey, false)

	// UUID-based key would never be matched by InvalidateAgent("my-agent")
	if strings.Contains(uuidKey, agentKey) {
		t.Error("UUID-based key should NOT contain agentKey")
	}
	if !strings.Contains(correctKey, agentKey) {
		t.Error("agentKey-based key MUST contain agentKey")
	}
}

// TestParseSessionKey_ExtractsAgentKey verifies ParseSessionKey returns the agentKey
// (the second segment), which should be a human-readable key, not a UUID.
func TestParseSessionKey_ExtractsAgentKey(t *testing.T) {
	tests := []struct {
		key       string
		wantAgent string
		wantRest  string
	}{
		{"agent:my-agent:heartbeat", "my-agent", "heartbeat"},
		{"agent:my-agent:cron:job-1", "my-agent", "cron:job-1"},
		{"agent:default:ws:direct:conv-1", "default", "ws:direct:conv-1"},
		{"agent:my-agent:team:t1:c1", "my-agent", "team:t1:c1"},
	}
	for _, tt := range tests {
		agentID, rest := ParseSessionKey(tt.key)
		if agentID != tt.wantAgent {
			t.Errorf("ParseSessionKey(%q) agentID = %q, want %q", tt.key, agentID, tt.wantAgent)
		}
		if rest != tt.wantRest {
			t.Errorf("ParseSessionKey(%q) rest = %q, want %q", tt.key, rest, tt.wantRest)
		}
	}
}
