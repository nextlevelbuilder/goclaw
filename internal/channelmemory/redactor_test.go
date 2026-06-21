package channelmemory

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestRedactorRedactsSecretsAndEmails(t *testing.T) {
	msgs := []store.PendingMessage{{
		ID:       uuid.New(),
		SenderID: "u1",
		Body:     "email me at owner@example.com token: sk-1234567890abcdef",
	}}
	got := NewRedactor().Redact(msgs, DefaultConfig())
	if len(got.Messages) != 1 {
		t.Fatalf("messages len = %d", len(got.Messages))
	}
	body := got.Messages[0].Body
	if strings.Contains(body, "owner@example.com") || strings.Contains(body, "sk-1234567890abcdef") {
		t.Fatalf("sensitive content not redacted: %q", body)
	}
	if !strings.Contains(body, "[REDACTED:email]") || !strings.Contains(body, "[REDACTED:secret]") {
		t.Fatalf("redaction markers missing: %q", body)
	}
}

func TestRedactorExcludesUsersAndPatterns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExcludeUsers = []string{"ignored"}
	cfg.ExcludePatterns = []string{"do not remember"}
	msgs := []store.PendingMessage{
		{ID: uuid.New(), SenderID: "ignored", Body: "project secret"},
		{ID: uuid.New(), SenderID: "kept", Body: "do not remember this"},
		{ID: uuid.New(), SenderID: "kept", Body: "remember this decision"},
	}
	got := NewRedactor().Redact(msgs, cfg)
	if len(got.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Body != "remember this decision" {
		t.Fatalf("kept body = %q", got.Messages[0].Body)
	}
	if got.Count != 2 {
		t.Fatalf("redaction count = %d, want 2", got.Count)
	}
}
