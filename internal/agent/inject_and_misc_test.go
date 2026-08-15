package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// ─── truncateForLog ───────────────────────────────────────────────────────

func TestTruncateForLog_ShortString(t *testing.T) {
	got := truncateForLog("hello", 100)
	if got != "hello" {
		t.Errorf("short string should be unchanged, got %q", got)
	}
}

func TestTruncateForLog_ExactLimit(t *testing.T) {
	s := strings.Repeat("x", 50)
	got := truncateForLog(s, 50)
	if got != s {
		t.Error("exact-limit string should be unchanged")
	}
}

func TestTruncateForLog_TruncatesLongString(t *testing.T) {
	s := strings.Repeat("a", 200)
	got := truncateForLog(s, 100)
	if !strings.HasSuffix(got, "...") {
		t.Error("truncated string should end with '...'")
	}
	if len(got) != 103 { // 100 + len("...")
		t.Errorf("truncated length = %d, want 103", len(got))
	}
}

// ─── processInjectedMessage ───────────────────────────────────────────────

func TestProcessInjectedMessage_PlainMessage(t *testing.T) {
	l := &Loop{}
	result, ok := l.processInjectedMessage(InjectedMessage{Content: "hello", UserID: "u1"}, nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.forSession.Content != "hello" {
		t.Errorf("forSession.Content = %q, want 'hello'", result.forSession.Content)
	}
	if !strings.Contains(result.forLLM.Content, "hello") {
		t.Error("forLLM should contain original message")
	}
	if !strings.Contains(result.forLLM.Content, "follow-up") {
		t.Error("forLLM should contain context wrapper")
	}
	if result.forLLM.Role != "user" {
		t.Errorf("forLLM.Role = %q, want 'user'", result.forLLM.Role)
	}
}

func TestProcessInjectedMessage_TruncatesOversizedContent(t *testing.T) {
	l := &Loop{maxMessageChars: 10}
	longMsg := strings.Repeat("X", 100)
	result, ok := l.processInjectedMessage(InjectedMessage{Content: longMsg, UserID: "u1"}, nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(result.forSession.Content, "[Message truncated]") {
		t.Error("expected truncation marker in forSession")
	}
}

func TestProcessInjectedMessage_UsesDefaultMaxCharsWhenZero(t *testing.T) {
	l := &Loop{maxMessageChars: 0}
	// A short message should not be truncated even with 0 (uses default).
	result, ok := l.processInjectedMessage(InjectedMessage{Content: "short", UserID: "u1"}, nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if strings.Contains(result.forSession.Content, "[Message truncated]") {
		t.Error("short message should not be truncated")
	}
}

func TestProcessInjectedMessage_EmitRunCalledWhenProvided(t *testing.T) {
	l := &Loop{}
	emitted := false
	emit := func(e AgentEvent) { emitted = true }
	_, ok := l.processInjectedMessage(InjectedMessage{Content: "msg", UserID: "u1"}, emit)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !emitted {
		t.Error("expected emitRun to be called")
	}
}

// ─── drainInjectChannel ───────────────────────────────────────────────────

func TestDrainInjectChannel_NilChannel(t *testing.T) {
	l := &Loop{}
	forLLM, forSession := l.drainInjectChannel(nil, nil)
	if forLLM != nil || forSession != nil {
		t.Error("nil channel should return nil slices")
	}
}

func TestDrainInjectChannel_EmptyChannel(t *testing.T) {
	l := &Loop{}
	ch := make(chan InjectedMessage, 5)
	forLLM, forSession := l.drainInjectChannel(ch, nil)
	if len(forLLM) != 0 || len(forSession) != 0 {
		t.Error("empty channel should return empty slices")
	}
}

func TestDrainInjectChannel_SingleMessage(t *testing.T) {
	l := &Loop{}
	ch := make(chan InjectedMessage, 5)
	ch <- InjectedMessage{Content: "hello", UserID: "u1"}

	forLLM, forSession := l.drainInjectChannel(ch, nil)
	if len(forLLM) != 1 || len(forSession) != 1 {
		t.Errorf("expected 1 message each, got %d/%d", len(forLLM), len(forSession))
	}
	if forSession[0].Content != "hello" {
		t.Errorf("forSession[0].Content = %q, want 'hello'", forSession[0].Content)
	}
}

func TestDrainInjectChannel_MultipleMessages(t *testing.T) {
	l := &Loop{}
	ch := make(chan InjectedMessage, 5)
	ch <- InjectedMessage{Content: "msg1", UserID: "u1"}
	ch <- InjectedMessage{Content: "msg2", UserID: "u1"}
	ch <- InjectedMessage{Content: "msg3", UserID: "u1"}

	forLLM, forSession := l.drainInjectChannel(ch, nil)
	if len(forLLM) != 3 || len(forSession) != 3 {
		t.Errorf("expected 3 messages each, got %d/%d", len(forLLM), len(forSession))
	}
}

// ─── filterBootstrapTools ─────────────────────────────────────────────────

func TestFilterBootstrapTools_AllowedNames(t *testing.T) {
	input := []string{"write_file", "read_file", "exec", "Write"}
	got := filterBootstrapTools(input)
	// Only write_file and Write are in allowlist
	for _, name := range got {
		if !bootstrapToolAllowlist[name] {
			t.Errorf("unexpected tool in filtered list: %q", name)
		}
	}
	found := map[string]bool{}
	for _, n := range got {
		found[n] = true
	}
	if !found["write_file"] {
		t.Error("write_file should be in bootstrap tools")
	}
}

func TestFilterBootstrapTools_EmptyInput(t *testing.T) {
	got := filterBootstrapTools(nil)
	if got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestFilterBootstrapTools_NoAllowedTools(t *testing.T) {
	got := filterBootstrapTools([]string{"exec", "read_file", "spawn"})
	if len(got) != 0 {
		t.Errorf("no allowed tools should return empty, got %v", got)
	}
}

// ─── agentToolPolicyForTeam ───────────────────────────────────────────────

func TestAgentToolPolicyForTeam_ReturnsUnchanged(t *testing.T) {
	p := &config.ToolPolicySpec{AlsoAllow: []string{"read_file"}}
	got := agentToolPolicyForTeam(p, true)
	if got != p {
		t.Error("agentToolPolicyForTeam should return the same pointer")
	}
}

func TestAgentToolPolicyForTeam_NilPolicyReturnNil(t *testing.T) {
	got := agentToolPolicyForTeam(nil, false)
	if got != nil {
		t.Errorf("nil policy should return nil, got %+v", got)
	}
}

// ─── extractUniqueSubmatch ────────────────────────────────────────────────

func TestExtractUniqueSubmatch_FilePaths(t *testing.T) {
	text := "See /home/user/project/main.go and /etc/config.yaml for details"
	// reFilePath captures group 1 (the path itself)
	matches := extractUniqueSubmatch(reFilePath, text, 1)
	found := false
	for _, m := range matches {
		if strings.Contains(m, "main.go") || strings.Contains(m, "config.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected file paths in submatch results, got %v", matches)
	}
}

func TestExtractUniqueSubmatch_Deduplicates(t *testing.T) {
	// Same path twice → should deduplicate
	text := "/home/user/file.go and /home/user/file.go again"
	matches := extractUniqueSubmatch(reFilePath, text, 1)
	count := 0
	for _, m := range matches {
		if strings.Contains(m, "file.go") {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected deduplication, got %d occurrences of file.go", count)
	}
}

func TestExtractUniqueSubmatch_EmptyText(t *testing.T) {
	matches := extractUniqueSubmatch(reFilePath, "", 1)
	if len(matches) != 0 {
		t.Errorf("empty text should return empty, got %v", matches)
	}
}
