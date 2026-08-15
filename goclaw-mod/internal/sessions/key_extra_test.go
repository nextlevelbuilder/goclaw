package sessions

import (
	"strings"
	"testing"
)

// TestBuildSessionKey covers the canonical DM and group formats.
func TestBuildSessionKey(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		channel string
		kind    PeerKind
		chatID  string
		want    string
	}{
		{
			name:    "DM session",
			agentID: "default",
			channel: "telegram",
			kind:    PeerDirect,
			chatID:  "386246614",
			want:    "agent:default:telegram:direct:386246614",
		},
		{
			name:    "group session",
			agentID: "default",
			channel: "telegram",
			kind:    PeerGroup,
			chatID:  "-100123456",
			want:    "agent:default:telegram:group:-100123456",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSessionKey(tt.agentID, tt.channel, tt.kind, tt.chatID)
			if got != tt.want {
				t.Errorf("BuildSessionKey = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildGroupTopicSessionKey covers forum topic key format.
func TestBuildGroupTopicSessionKey(t *testing.T) {
	got := BuildGroupTopicSessionKey("default", "telegram", "-100123456", 99)
	want := "agent:default:telegram:group:-100123456:topic:99"
	if got != want {
		t.Errorf("BuildGroupTopicSessionKey = %q, want %q", got, want)
	}
}

// TestBuildDMThreadSessionKey covers DM thread key format.
func TestBuildDMThreadSessionKey(t *testing.T) {
	got := BuildDMThreadSessionKey("my-agent", "telegram", "386246614", 7)
	want := "agent:my-agent:telegram:direct:386246614:thread:7"
	if got != want {
		t.Errorf("BuildDMThreadSessionKey = %q, want %q", got, want)
	}
}

// TestBuildScopedThreadSessionKey covers string-based thread IDs (Slack timestamps).
func TestBuildScopedThreadSessionKey(t *testing.T) {
	got := BuildScopedThreadSessionKey("bot", "slack", PeerDirect, "U12345", "1712345678.000100")
	want := "agent:bot:slack:direct:U12345:thread:1712345678.000100"
	if got != want {
		t.Errorf("BuildScopedThreadSessionKey = %q, want %q", got, want)
	}
}

// TestBuildSubagentSessionKey covers the subagent key format.
func TestBuildSubagentSessionKey(t *testing.T) {
	got := BuildSubagentSessionKey("default", "my-task")
	want := "agent:default:subagent:my-task"
	if got != want {
		t.Errorf("BuildSubagentSessionKey = %q, want %q", got, want)
	}
}

// TestBuildTeamSessionKey covers team session key format.
func TestBuildTeamSessionKey(t *testing.T) {
	got := BuildTeamSessionKey("my-agent", "team-42", "chat-99")
	want := "agent:my-agent:team:team-42:chat-99"
	if got != want {
		t.Errorf("BuildTeamSessionKey = %q, want %q", got, want)
	}
}

// TestIsTeamSession distinguishes team vs non-team keys.
func TestIsTeamSession(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"agent:my-agent:team:t1:c1", true},
		{"agent:my-agent:cron:job-1", false},
		{"agent:my-agent:subagent:label", false},
		{"agent:my-agent:heartbeat", false},
		{"not-a-session-key", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsTeamSession(tt.key); got != tt.want {
				t.Errorf("IsTeamSession(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestBuildCronSessionKey_DoublePrefix guards against double-prefixing.
func TestBuildCronSessionKey_DoublePrefix(t *testing.T) {
	// If jobID is already a canonical session key, only the rest part is used.
	canonical := "agent:my-agent:cron:existing-job"
	got := BuildCronSessionKey("other-agent", canonical)
	// Should use "cron:existing-job" as rest, not re-wrap the whole canonical key.
	if strings.Contains(got, "agent:my-agent") {
		t.Errorf("double-prefix not guarded: got %q", got)
	}
	if !strings.HasPrefix(got, "agent:other-agent:") {
		t.Errorf("expected other-agent prefix, got %q", got)
	}
}

// TestBuildAgentMainSessionKey covers default and custom main keys.
func TestBuildAgentMainSessionKey(t *testing.T) {
	tests := []struct {
		agentID string
		mainKey string
		want    string
	}{
		{"my-agent", "", "agent:my-agent:main"},
		{"my-agent", "custom-main", "agent:my-agent:custom-main"},
	}
	for _, tt := range tests {
		t.Run(tt.mainKey, func(t *testing.T) {
			got := BuildAgentMainSessionKey(tt.agentID, tt.mainKey)
			if got != tt.want {
				t.Errorf("BuildAgentMainSessionKey = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildScopedSessionKey delegates to BuildSessionKey; verify output matches.
func TestBuildScopedSessionKey(t *testing.T) {
	got := BuildScopedSessionKey("default", "telegram", PeerGroup, "-100123")
	want := BuildSessionKey("default", "telegram", PeerGroup, "-100123")
	if got != want {
		t.Errorf("BuildScopedSessionKey = %q, want %q", got, want)
	}
}

// TestIsSubagentSession distinguishes subagent vs non-subagent keys.
func TestIsSubagentSession(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"agent:default:subagent:my-label", true},
		{"agent:default:SUBAGENT:label", true}, // case-insensitive
		{"agent:default:cron:job-1", false},
		{"agent:default:team:t1:c1", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsSubagentSession(tt.key); got != tt.want {
				t.Errorf("IsSubagentSession(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestIsCronSession distinguishes cron vs non-cron keys.
func TestIsCronSession(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"agent:default:cron:reminder-123", true},
		{"agent:default:CRON:job", true}, // case-insensitive
		{"agent:default:subagent:x", false},
		{"agent:default:heartbeat", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsCronSession(tt.key); got != tt.want {
				t.Errorf("IsCronSession(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestIsHeartbeatSession distinguishes heartbeat vs other keys.
func TestIsHeartbeatSession(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"agent:default:heartbeat", true},
		{"agent:default:heartbeat:1712345678000", true},
		{"agent:default:cron:job", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsHeartbeatSession(tt.key); got != tt.want {
				t.Errorf("IsHeartbeatSession(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestBuildWSSessionKey and TestIsWSSession cover WS key helpers.
func TestBuildWSSessionKey(t *testing.T) {
	got := BuildWSSessionKey("default", "conv-abc")
	want := "agent:default:ws:direct:conv-abc"
	if got != want {
		t.Errorf("BuildWSSessionKey = %q, want %q", got, want)
	}
}

func TestIsWSSession(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"agent:default:ws:direct:conv-1", true},
		{"agent:default:ws-legacy:room-2", true}, // legacy ws- prefix
		{"agent:default:telegram:direct:123", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsWSSession(tt.key); got != tt.want {
				t.Errorf("IsWSSession(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestPeerKindFromGroup covers the helper.
func TestPeerKindFromGroup(t *testing.T) {
	if PeerKindFromGroup(true) != PeerGroup {
		t.Error("expected PeerGroup for isGroup=true")
	}
	if PeerKindFromGroup(false) != PeerDirect {
		t.Error("expected PeerDirect for isGroup=false")
	}
}

// TestParseSessionKey_InvalidFormats covers non-canonical keys.
func TestParseSessionKey_InvalidFormats(t *testing.T) {
	tests := []string{
		"",
		"noprefix",
		"agent:",
		"agent:only-one-part",
		"other:prefix:rest",
	}
	for _, key := range tests {
		agentID, rest := ParseSessionKey(key)
		if key == "agent:only-one-part" {
			// Only 2 parts when split by ":" with N=3 — depends on SplitN behavior
			// SplitN("agent:only-one-part", ":", 3) → ["agent", "only-one-part"] len=2 < 3 → ("","")
			if agentID != "" || rest != "" {
				t.Errorf("ParseSessionKey(%q) = (%q, %q), want ('', '')", key, agentID, rest)
			}
			continue
		}
		if agentID != "" || rest != "" {
			t.Errorf("ParseSessionKey(%q) = (%q, %q), want ('', '')", key, agentID, rest)
		}
	}
}
