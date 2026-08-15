package agent

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
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
	inputTokens      int64
	outputTokens     int64
	setLastTokens    int
	setLastMsgCount  int

	// Configurable/recording fields for compaction-count and truncation tests.
	// Guarded by mu because maybeSummarize mutates them from a background goroutine.
	mu               sync.Mutex
	summary          string // returned by GetSummary
	compactionCount  int    // returned by GetCompactionCount
	incrementCalls   int    // counts IncrementCompaction invocations
	truncateCalls    int    // counts TruncateHistory invocations
	truncateKeepLast int    // last keepLast passed to TruncateHistory
	setSummaryCalls  int    // counts SetSummary invocations
	lastSetSummary   string // last summary passed to SetSummary
}

// SessionCoreStore methods
func (n *nopSessionStore) GetOrCreate(_ context.Context, _ string) *store.SessionData {
	return &store.SessionData{}
}
func (n *nopSessionStore) Get(_ context.Context, _ string) *store.SessionData          { return nil }
func (n *nopSessionStore) AddMessage(_ context.Context, _ string, _ providers.Message) {}
func (n *nopSessionStore) GetHistory(_ context.Context, _ string) []providers.Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.history
}
func (n *nopSessionStore) GetSummary(_ context.Context, _ string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.summary
}
func (n *nopSessionStore) SetSummary(_ context.Context, _, s string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.setSummaryCalls++
	n.lastSetSummary = s
}
func (n *nopSessionStore) GetLabel(_ context.Context, _ string) string                     { return "" }
func (n *nopSessionStore) SetLabel(_ context.Context, _, _ string)                         {}
func (n *nopSessionStore) SetAgentInfo(_ context.Context, _ string, _ uuid.UUID, _ string) {}
func (n *nopSessionStore) TruncateHistory(_ context.Context, _ string, keepLast int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.truncateCalls++
	n.truncateKeepLast = keepLast
	// Mirror real-store semantics: keep only the last keepLast messages so
	// anti-loop tests observe an actually-shrunken history on the next turn.
	if keepLast >= 0 && keepLast < len(n.history) {
		n.history = n.history[len(n.history)-keepLast:]
	}
}
func (n *nopSessionStore) SetHistory(_ context.Context, _ string, msgs []providers.Message) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.history = msgs
}
func (n *nopSessionStore) Reset(_ context.Context, _ string)                               {}
func (n *nopSessionStore) Delete(_ context.Context, _ string) error                        { return nil }
func (n *nopSessionStore) Save(_ context.Context, _ string) error                          { return nil }

// SessionMetadataStore methods
func (n *nopSessionStore) UpdateMetadata(_ context.Context, _, _, _, _ string) {}
func (n *nopSessionStore) AccumulateTokens(_ context.Context, _ string, input, output int64) {
	n.inputTokens += input
	n.outputTokens += output
}
func (n *nopSessionStore) IncrementCompaction(_ context.Context, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.incrementCalls++
	n.compactionCount++
}
func (n *nopSessionStore) GetCompactionCount(_ context.Context, _ string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.compactionCount
}
func (n *nopSessionStore) GetMemoryFlushCompactionCount(_ context.Context, _ string) int { return 0 }
func (n *nopSessionStore) SetMemoryFlushDone(_ context.Context, _ string)                {}
func (n *nopSessionStore) GetSessionMetadata(_ context.Context, _ string) map[string]string {
	return nil
}
func (n *nopSessionStore) SetSessionMetadata(_ context.Context, _ string, _ map[string]string) {}
func (n *nopSessionStore) SetSpawnInfo(_ context.Context, _, _ string, _ int)                  {}
func (n *nopSessionStore) SetContextWindow(_ context.Context, _ string, _ int)                 {}
func (n *nopSessionStore) GetContextWindow(_ context.Context, _ string) int                    { return 0 }
func (n *nopSessionStore) SetLastPromptTokens(_ context.Context, _ string, tokens, msgCount int) {
	n.setLastTokens = tokens
	n.setLastMsgCount = msgCount
	n.lastPromptTokens = tokens
	n.lastMsgCount = msgCount
}
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

// Thread-safe accessors for recording fields (maybeSummarize mutates them from a
// background goroutine, so tests must not read the fields directly under -race).
func (n *nopSessionStore) truncateCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.truncateCalls
}
func (n *nopSessionStore) incrementCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.incrementCalls
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

	// Agent-only budget: the calling agent reserves a FIXED maxTokens from the
	// window, so compactionInputCap() = min(window-maxTokens, share*window-maxTokens).
	// With window=10000, maxTokens=1024 → inputCap = min(8976, 7476) = 7476.
	const maxTokens = 1024

	// Build history large enough to exceed the compaction threshold while still
	// packing the to-summarize slice into a single chunk under the input cap.
	// threshold = contextWindow * DefaultHistoryShare = 10000 * 0.85 = 8500.
	// estimateMessageTokens ≈ runes/3; 10 msgs × 3000 chars → ~10000 tokens > 8500.
	// The 6 to-summarize messages pack into one ~6520-token chunk (< 7476 cap).
	longContent := makeLongString(3000)
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
		maxTokens:     maxTokens,
		sessions:      sessions,
		// hasMemory = false → shouldRunMemoryFlush returns false (skip memory flush)
		hasMemory: false,
		// compactionCfg nil → uses DefaultHistoryShare (0.85), keepLast=4
		compactionCfg: nil,
		// tokenCounter nil → estimateSummaryInputTokens uses rune/3 fallback
	}

	loop.maybeSummarize(context.Background(), "test-session-key", false)

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

	gotMaxTokens, ok := maxTokensRaw.(int)
	if !ok {
		t.Fatalf("Options[\"max_tokens\"] type = %T, want int", maxTokensRaw)
	}

	// Compute expected using the same formula the implementation uses.
	// keepLast=4, history has 10 messages → toSummarize = history[:6].
	// tokenCounter nil → rune/3 fallback on the fixture content.
	toSummarize := history[:len(history)-4]
	expectedIn := loop.estimateSummaryInputTokens(toSummarize)
	wantMax := dynamicSummaryMax(expectedIn)
	if gotMaxTokens != wantMax {
		t.Errorf("max_tokens = %d, want %d (dynamicSummaryMax(%d))", gotMaxTokens, wantMax, expectedIn)
	}
}

func TestMaybeSummarize_LogsSkipDecisionUnderThreshold(t *testing.T) {
	sessions := &nopSessionStore{
		history: []providers.Message{
			{Role: "user", Content: "short request"},
			{Role: "assistant", Content: "short response"},
		},
		lastPromptTokens: 0,
		lastMsgCount:     0,
	}
	loop := &Loop{
		id:            "test-agent",
		provider:      &capturingProvider{response: "unused"},
		model:         "claude-3-5-sonnet",
		contextWindow: 10000,
		sessions:      sessions,
		hasMemory:     false,
	}

	logs := captureSlog(t, func() {
		loop.maybeSummarize(context.Background(), "test-session-key", false)
	})

	assertLogContains(t, logs,
		"compaction_decision",
		`"path":"post-turn"`,
		`"decision":"skip"`,
		`"skip_reason":"under_threshold"`,
		`"agent":"test-agent"`,
		`"session":"test-session-key"`,
		`"context_window":10000`,
	)
}

func TestMaybeSummarize_IgnoresInvalidLastPromptCalibration(t *testing.T) {
	provider := &capturingProvider{response: "unused"}
	sessions := &nopSessionStore{
		history: []providers.Message{
			{Role: "user", Content: "short request"},
			{Role: "assistant", Content: "short response"},
		},
		lastPromptTokens: 264722,
		lastMsgCount:     28,
	}
	loop := &Loop{
		id:            "test-agent",
		provider:      provider,
		model:         "claude-3-5-sonnet",
		contextWindow: 200000,
		sessions:      sessions,
		hasMemory:     false,
	}

	logs := captureSlog(t, func() {
		loop.maybeSummarize(context.Background(), "test-session-key", false)
	})

	if len(provider.captured) != 0 {
		t.Fatalf("provider.Chat called %d times, want 0 for invalid calibration under fallback threshold", len(provider.captured))
	}
	assertLogContains(t, logs,
		"compaction_decision",
		`"decision":"skip"`,
		`"skip_reason":"under_threshold"`,
		`"last_prompt_tokens":264722`,
		`"calibration_invalid":true`,
		`"invalid_last_prompt_tokens":264722`,
	)
}

func TestMaybeSummarize_LogsTriggerDecisionOverThreshold(t *testing.T) {
	const contextWindow = 10000
	// Fixed agent reserve so compactionInputCap() leaves room to pack the
	// to-summarize slice into one chunk (see TestMaybeSummarize_MaxTokensDynamic).
	const maxTokens = 1024

	longContent := makeLongString(3000)
	history := make([]providers.Message, 10)
	for i := range history {
		if i%2 == 0 {
			history[i] = providers.Message{Role: "user", Content: longContent}
		} else {
			history[i] = providers.Message{Role: "assistant", Content: longContent}
		}
	}

	done := make(chan struct{}, 1)
	provider := &signallingProvider{
		capturingProvider: capturingProvider{response: "compaction summary"},
		done:              done,
	}
	sessions := &nopSessionStore{
		history:          history,
		lastPromptTokens: 0,
		lastMsgCount:     0,
	}
	loop := &Loop{
		id:            "test-agent",
		provider:      provider,
		model:         "claude-3-5-sonnet",
		contextWindow: contextWindow,
		maxTokens:     maxTokens,
		sessions:      sessions,
		hasMemory:     false,
	}

	logs := captureSlog(t, func() {
		loop.maybeSummarize(context.Background(), "test-session-key", false)

		// Wait for the background summarize goroutine to reach the point where it
		// calls provider.Chat (which happens strictly after the "compact_budget"
		// slog.Info write). This must happen INSIDE the captureSlog closure so the
		// buffer is not read until the background goroutine's logging is done —
		// otherwise buf.String() races with the goroutine's concurrent slog.Info
		// write to the same bytes.Buffer under -race.
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for maybeSummarize to call provider.Chat")
		}
	})

	assertLogContains(t, logs,
		"compaction_decision",
		`"path":"post-turn"`,
		`"decision":"trigger"`,
		`"agent":"test-agent"`,
		`"session":"test-session-key"`,
		`"context_window":10000`,
	)
}

// makeLongString returns a string of n ASCII characters ('a').
func makeLongString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func captureSlog(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	fn()
	return buf.String()
}

func assertLogContains(t *testing.T, logs string, fragments ...string) {
	t.Helper()

	for _, fragment := range fragments {
		if !strings.Contains(logs, fragment) {
			t.Fatalf("logs do not contain %q\nlogs:\n%s", fragment, logs)
		}
	}
}
