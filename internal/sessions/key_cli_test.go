package sessions

import "testing"

// The CLI session form is the one key in this package that is NOT an agent
// session. These tests pin both halves of that: it round-trips through its own
// builder/parser, and none of the existing agent-key helpers claim it.

func TestCLISessionKeyRoundTrip(t *testing.T) {
	const connID = "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"
	const convID = "11112222-3333-4444-5555-666677778888"

	key := BuildCLISessionKey(connID, convID)
	if want := "cli:" + connID + ":" + convID; key != want {
		t.Fatalf("BuildCLISessionKey = %q, want %q", key, want)
	}

	gotConn, gotConv, ok := ParseCLISessionKey(key)
	if !ok {
		t.Fatalf("ParseCLISessionKey(%q) reported not-ok", key)
	}
	if gotConn != connID {
		t.Errorf("connectionID = %q, want %q", gotConn, connID)
	}
	if gotConv != convID {
		t.Errorf("conversationID = %q, want %q", gotConv, convID)
	}
	if !IsCLISession(key) {
		t.Errorf("IsCLISession(%q) = false, want true", key)
	}
}

func TestParseCLISessionKeyRejectsUnusable(t *testing.T) {
	// Each of these must be refused: a caller must never open a CLI session
	// against an empty connection or conversation id.
	cases := map[string]string{
		"empty":              "",
		"prefix only":        "cli:",
		"no conversation":    "cli:conn-1",
		"empty connection":   "cli::conv-1",
		"empty conversation": "cli:conn-1:",
		"agent key":          "agent:default:ws:direct:conv-1",
		"wrong head":         "clix:conn-1:conv-1",
	}
	for name, key := range cases {
		if conn, conv, ok := ParseCLISessionKey(key); ok {
			t.Errorf("%s: ParseCLISessionKey(%q) = (%q, %q, true), want not-ok", name, key, conn, conv)
		}
	}
}

// TestIsCLISessionIsPrefixNotParse locks in the deliberate asymmetry: routing
// claims anything headed "cli:", so a malformed CLI key reaches the CLI handler
// (which explains what is wrong) instead of silently starting an agent run.
func TestIsCLISessionIsPrefixNotParse(t *testing.T) {
	const malformed = "cli:"
	if _, _, ok := ParseCLISessionKey(malformed); ok {
		t.Fatalf("precondition: %q should not parse", malformed)
	}
	if !IsCLISession(malformed) {
		t.Errorf("IsCLISession(%q) = false, want true (routing must claim it)", malformed)
	}
	if IsCLISession("agent:default:ws:direct:x") {
		t.Errorf("IsCLISession claimed an agent key")
	}
}

// TestCLIKeyIsNotAnAgentKey is the important one: every existing helper that
// derives meaning from an "agent:"-headed key must decline a CLI key, so adding
// this form cannot change how any current session is treated.
func TestCLIKeyIsNotAnAgentKey(t *testing.T) {
	key := BuildCLISessionKey("conn-1", "conv-1")

	if agentID, rest := ParseSessionKey(key); agentID != "" || rest != "" {
		t.Errorf("ParseSessionKey(%q) = (%q, %q), want empty/empty", key, agentID, rest)
	}
	for name, got := range map[string]bool{
		"IsWSSession":        IsWSSession(key),
		"IsSubagentSession":  IsSubagentSession(key),
		"IsCronSession":      IsCronSession(key),
		"IsTeamSession":      IsTeamSession(key),
		"IsHeartbeatSession": IsHeartbeatSession(key),
	} {
		if got {
			t.Errorf("%s(%q) = true, want false", name, key)
		}
	}
}

// TestAgentKeysAreNotCLISessions is the converse: no shape the existing builders
// produce may be mistaken for a CLI conversation.
func TestAgentKeysAreNotCLISessions(t *testing.T) {
	keys := []string{
		BuildSessionKey("default", "telegram", PeerDirect, "386246614"),
		BuildSessionKey("cli", "telegram", PeerDirect, "1"), // an agent literally named "cli"
		BuildGroupTopicSessionKey("default", "telegram", "-100123", 99),
		BuildDMThreadSessionKey("default", "telegram", "42", 7),
		BuildScopedThreadSessionKey("default", "slack", PeerGroup, "C1", "ts-1"),
		BuildSubagentSessionKey("default", "my-task"),
		BuildTeamSessionKey("default", "team-1", "chat-1"),
		BuildCronSessionKey("default", "job-1"),
		BuildAgentMainSessionKey("default", ""),
		BuildWSSessionKey("default", "conv-1"),
		BuildHeartbeatSessionKey("default", false),
	}
	for _, key := range keys {
		if IsCLISession(key) {
			t.Errorf("IsCLISession(%q) = true, want false", key)
		}
		if _, _, ok := ParseCLISessionKey(key); ok {
			t.Errorf("ParseCLISessionKey(%q) parsed an agent key", key)
		}
	}
}
