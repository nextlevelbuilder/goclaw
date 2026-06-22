package agent

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type deadlineCapturingProvider struct {
	capturingProvider
	deadline    time.Time
	hasDeadline bool
}

func (d *deadlineCapturingProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	d.deadline, d.hasDeadline = ctx.Deadline()
	return d.capturingProvider.Chat(ctx, req)
}

func TestCompactMessagesInPlace_UsesDefaultTimeout(t *testing.T) {
	provider := &deadlineCapturingProvider{
		capturingProvider: capturingProvider{response: "Summary of conversation."},
	}
	loop := &Loop{
		provider: provider,
		model:    "claude-3-5-sonnet",
	}

	start := time.Now()
	result := loop.compactMessagesInPlace(context.Background(), compactableMessages())
	if result == nil {
		t.Fatal("compactMessagesInPlace returned nil; expected compaction to succeed")
	}

	assertDeadlineWithin(t, provider, start, 120*time.Second)
}

func TestCompactMessagesInPlace_UsesConfiguredTimeout(t *testing.T) {
	provider := &deadlineCapturingProvider{
		capturingProvider: capturingProvider{response: "Summary of conversation."},
	}
	loop := &Loop{
		provider: provider,
		model:    "claude-3-5-sonnet",
		compactionCfg: &config.CompactionConfig{
			TimeoutSeconds: 45,
		},
	}

	start := time.Now()
	result := loop.compactMessagesInPlace(context.Background(), compactableMessages())
	if result == nil {
		t.Fatal("compactMessagesInPlace returned nil; expected compaction to succeed")
	}

	assertDeadlineWithin(t, provider, start, 45*time.Second)
}

func TestCompactMessagesInPlace_NonPositiveTimeoutFallsBackToDefault(t *testing.T) {
	provider := &deadlineCapturingProvider{
		capturingProvider: capturingProvider{response: "Summary of conversation."},
	}
	loop := &Loop{
		provider: provider,
		model:    "claude-3-5-sonnet",
		compactionCfg: &config.CompactionConfig{
			TimeoutSeconds: -1,
		},
	}

	start := time.Now()
	result := loop.compactMessagesInPlace(context.Background(), compactableMessages())
	if result == nil {
		t.Fatal("compactMessagesInPlace returned nil; expected compaction to succeed")
	}

	assertDeadlineWithin(t, provider, start, 120*time.Second)
}

func compactableMessages() []providers.Message {
	msgs := make([]providers.Message, 10)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = providers.Message{Role: "user", Content: "user message"}
		} else {
			msgs[i] = providers.Message{Role: "assistant", Content: "assistant reply"}
		}
	}
	return msgs
}

func assertDeadlineWithin(t *testing.T, provider *deadlineCapturingProvider, start time.Time, want time.Duration) {
	t.Helper()

	if !provider.hasDeadline {
		t.Fatal("provider context has no deadline")
	}

	got := provider.deadline.Sub(start)
	lower := want - time.Second
	upper := want + time.Second
	if got < lower || got > upper {
		t.Fatalf("deadline duration = %s, want within [%s, %s]", got, lower, upper)
	}
}
