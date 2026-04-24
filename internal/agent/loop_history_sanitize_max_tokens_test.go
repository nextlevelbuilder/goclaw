package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// nopSessionStore is a minimal no-op implementation of store.SessionStore
// for testing maybeSummarize without a real database.
// All methods return zero values except GetHistory and GetLastPromptTokens,
// which return controlled fixture data.
type nopSessionStore struct {
	history          []providers.Message
	lastPromptTokens int
	lastMsgCount     int
}

// SessionCoreStore methods
func (n *nopSessionStore) GetOrCreate(_ context.Context, _ string) *store.SessionData {
	return &store.SessionData{}
}
func (n *nopSessionStore) Get(_ context.Context, _ string) *store.SessionData { return nil }
func (n *nopSessionStore) AddMessage(_ context.Context, _ string, _ providers.Message) {}
func (n *nopSessionStore) GetHistory(_ context.Context, _ string) []providers.Message {
	return n.history
}
func (n *nopSessionStore) GetSummary(_ context.Context, _ string) string   { return "" }
func (n *nopSessionStore) SetSummary(_ context.Context, _, _ string)       {}
func (n *nopSessionStore) GetLabel(_ context.Context, _ string) string     { return "" }
func (n *nopSessionStore) SetLabel(_ context.Context, _, _ string)         {}
func (n *nopSessionStore) SetAgentInfo(_ context.Context, _ string, _ uuid.UUID, _ string) {}
func (n *nopSessionStore) TruncateHistory(_ context.Context, _ string, _ int) {}
func (n *nopSessionStore) SetHistory(_ context.Context, _ string, _ []providers.Message) {}
func (n *nopSessionStore) Reset(_ context.Context, _ string)               {}
func (n *nopSessionStore) Delete(_ context.Context, _ string) error        { return nil }
func (n *nopSessionStore) Save(_ context.Context, _ string) error          { return nil }

// SessionMetadataStore methods
func (n *nopSessionStore) UpdateMetadata(_ context.Context, _, _, _, _ string)      {}
func (n *nopSessionStore) AccumulateTokens(_ context.Context, _ string, _, _ int64) {}
func (n *nopSessionStore) IncrementCompaction(_ context.Context, _ string)           {}
func (n *nopSessionStore) GetCompactionCount(_ context.Context, _ string) int        { return 0 }
func (n *nopSessionStore) GetMemoryFlushCompactionCount(_ context.Context, _ string) int { return 0 }
func (n *nopSessionStore) SetMemoryFlushDone(_ context.Context, _ string)            {}
func (n *nopSessionStore) GetSessionMetadata(_ context.Context, _ string) map[string]string {
	return nil
}
func (n *nopSessionStore) SetSessionMetadata(_ context.Context, _ string, _ map[string]string) {}
func (n *nopSessionStore) SetSpawnInfo(_ context.Context, _, _ string, _ int)                  {}
func (n *nopSessionStore) SetContextWindow(_ context.Context, _ string, _ int)                 {}
func (n *nopSessionStore) GetContextWindow(_ context.Context, _ string) int                    { return 0 }
func (n *nopSessionStore) SetLastPromptTokens(_ context.Context, _ string, _, _ int)           {}
func (n *nopSessionStore) GetLastPromptTokens(_ context.Context, _ string) (int, int) {
	return n.lastPromptTokens, n.lastMsgCount
}

// SessionListingStore methods
func (n *nopSessionStore) List(_ context.Context, _ string) []store.SessionInfo { return nil }
func (n *nopSessionStore) ListPaged(_ context.Context, _ store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{Sessions: []store.SessionInfo{}}
}
func (n *nopSessionStore) ListPagedRich(_ context.Context, _ store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}}
}
func (n *nopSessionStore) LastUsedChannel(_ context.Context, _ string) (string, string) {
	return "", ""
}

// signallingProvider wraps capturingProvider and signals a channel when Chat is called.
type signallingProvider struct {
	capturingProvider
	done chan struct{}
}

func (s *signallingProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	resp, err := s.capturingProvider.Chat(ctx, req)
	select {
	case s.done <- struct{}{}:
	default:
	}
	return resp, err
}

// TestMaybeSummarize_MaxTokensDynamic verifies that maybeSummarize passes
// max_tokens == dynamicSummaryMax(estimatedInputTokens) to the provider.
func TestMaybeSummarize_MaxTokensDynamic(t *testing.T) {
	const contextWindow = 10000

	// Build history large enough to exceed the compaction threshold.
	// threshold = contextWindow * DefaultHistoryShare = 10000 * 0.85 = 8500.
	// EstimateTokens uses ~4 chars/token; 9000 tokens * 4 = 36000 chars of content.
	// Use 5 user-assistant pairs each carrying ~9000 chars so EstimateTokens > threshold.
	longContent := makeLongString(9000)
	history := make([]providers.Message, 10)
	for i := range history {
		if i%2 == 0 {
			history[i] = providers.Message{Role: "user", Content: longContent}
		} else {
			history[i] = providers.Message{Role: "assistant", Content: longContent}
		}
	}

	done := make(chan struct{}, 1)
	sp := &signallingProvider{
		capturingProvider: capturingProvider{response: "compaction summary"},
		done:              done,
	}

	sessions := &nopSessionStore{
		history:          history,
		lastPromptTokens: 0, // no calibration → falls back to EstimateTokens
		lastMsgCount:     0,
	}

	loop := &Loop{
		provider:      sp,
		model:         "claude-3-5-sonnet",
		contextWindow: contextWindow,
		sessions:      sessions,
		// hasMemory = false → shouldRunMemoryFlush returns false (skip memory flush)
		hasMemory:     false,
		// compactionCfg nil → uses DefaultHistoryShare (0.85), keepLast=4
		compactionCfg: nil,
		// tokenCounter nil → estimateSummaryInputTokens uses rune/3 fallback
	}

	loop.maybeSummarize(context.Background(), "test-session-key")

	// Wait for background goroutine to call provider.Chat (up to 5s).
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for maybeSummarize to call provider.Chat")
	}

	if len(sp.captured) == 0 {
		t.Fatal("provider.Chat was not called")
	}

	req := sp.captured[0]
	maxTokensRaw, ok := req.Options["max_tokens"]
	if !ok {
		t.Fatal("Options[\"max_tokens\"] not set in ChatRequest from maybeSummarize")
	}

	maxTokens, ok := maxTokensRaw.(int)
	if !ok {
		t.Fatalf("Options[\"max_tokens\"] type = %T, want int", maxTokensRaw)
	}

	// Compute expected using the same formula the implementation uses.
	// keepLast=4, history has 10 messages → toSummarize = history[:6].
	// tokenCounter nil → rune/3 fallback on the fixture content.
	toSummarize := history[:len(history)-4]
	expectedIn := loop.estimateSummaryInputTokens(toSummarize)
	wantMax := dynamicSummaryMax(expectedIn)
	if maxTokens != wantMax {
		t.Errorf("max_tokens = %d, want %d (dynamicSummaryMax(%d))", maxTokens, wantMax, expectedIn)
	}
}

// makeLongString returns a string of n ASCII characters ('a').
func makeLongString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
