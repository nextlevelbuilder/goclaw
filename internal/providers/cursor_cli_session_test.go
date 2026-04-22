package providers

import (
	"strings"
	"testing"
)

func TestCursorBuildArgsDoesNotUseSessionIDFlag(t *testing.T) {
	p := NewCursorCLIProvider("agent")
	workDir := t.TempDir()

	args := p.buildArgs("gpt-5.4", workDir, "", false)
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "--session-id") {
		t.Fatalf("buildArgs() includes unsupported --session-id flag: %q", joined)
	}
}

func TestCursorBuildArgsWithChatID(t *testing.T) {
	p := NewCursorCLIProvider("agent")
	workDir := t.TempDir()

	args := p.buildArgs("gpt-5.4", workDir, "chat-abc123", false)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--resume chat-abc123") {
		t.Fatalf("buildArgs() should include --resume with chat ID: %q", joined)
	}
}

func TestCursorBuildArgsWithoutChatID(t *testing.T) {
	p := NewCursorCLIProvider("agent")
	workDir := t.TempDir()

	args := p.buildArgs("gpt-5.4", workDir, "", false)
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "--resume") {
		t.Fatalf("buildArgs() should not include --resume when chatID is empty: %q", joined)
	}
}
