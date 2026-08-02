package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// deadlineBurningProvider models the real failure this test exists for: on a
// large session the summarizer request consumes its entire timeout and then
// fails. It waits for the caller's context to expire, so the error it returns is
// the context's own — exactly what the live log showed
// (`summarization failed error="context deadline exceeded"`).
type deadlineBurningProvider struct{}

func (p *deadlineBurningProvider) Chat(ctx context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *deadlineBurningProvider) ChatStream(ctx context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *deadlineBurningProvider) DefaultModel() string { return "deadline-burning-model" }
func (p *deadlineBurningProvider) Name() string         { return "deadline-burning" }

// recordingSaveStore records whether Save saw a live context, and can be told to
// fail the way a real store does when handed an expired one.
type recordingSaveStore struct {
	*nopSessionStore
	saveCalls  int
	saveCtxErr error
	saveErr    error
}

func (r *recordingSaveStore) Save(ctx context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saveCalls++
	r.saveCtxErr = ctx.Err()
	// Mirror database/sql: a dead context fails the write immediately.
	if ctx.Err() != nil {
		r.saveErr = ctx.Err()
		return ctx.Err()
	}
	return nil
}

func (r *recordingSaveStore) saveState() (int, error, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveCalls, r.saveCtxErr, r.saveErr
}

// The extractive fallback exists so a timed-out summarizer still shrinks
// history. But the persist calls reused the summarizer's context — the very one
// the timeout had just killed — so every write ran on a dead context, Save's
// ExecContext failed instantly, and its error was discarded. The fallback
// computed a perfectly good summary and threw it away: history never shrank,
// compaction_count never advanced, and the next turn had to fall back again.
// Persist must run on a fresh context.
func TestMaybeSummarizePersistsAfterSummarizerTimeout(t *testing.T) {
	sessions := &recordingSaveStore{
		nopSessionStore: &nopSessionStore{history: buildTokenHistory(20, 200000)},
	}
	// Built inline rather than via newPressureLoop: that helper takes the concrete
	// *nopSessionStore, and this test needs the Save-recording wrapper.
	loop := &Loop{
		id:            "test-agent",
		provider:      &deadlineBurningProvider{},
		model:         "claude-3-5-sonnet",
		contextWindow: pressureContextWindow,
		maxTokens:     pressureMaxTokens,
		sessions:      sessions,
		hasMemory:     false,
		// 1s timeout so the summarizer burns it quickly instead of waiting 120s.
		compactionCfg: &config.CompactionConfig{TimeoutSeconds: 1},
	}

	loop.maybeSummarize(context.Background(), "sess-timeout", false)

	// The fallback must still persist: Save reached, on a LIVE context.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if calls, _, _ := sessions.saveState(); calls > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	calls, ctxErr, saveErr := sessions.saveState()
	if calls == 0 {
		t.Fatal("Save never called; the extractive fallback's summary was discarded and history stays oversized forever")
	}
	if ctxErr != nil {
		t.Errorf("Save ran on an expired context (%v); persist must not reuse the summarizer's timed-out context", ctxErr)
	}
	if saveErr != nil {
		t.Errorf("Save failed: %v", saveErr)
	}
	if got := sessions.incrementCount(); got != 1 {
		t.Errorf("IncrementCompaction called %d times, want 1 — compaction_count must advance or the session re-falls-back every turn", got)
	}
	if got := sessions.truncateCount(); got != 1 {
		t.Errorf("TruncateHistory called %d times, want 1 — stored history must actually shrink", got)
	}
	sessions.mu.Lock()
	summary := sessions.lastSetSummary
	sessions.mu.Unlock()
	if summary == "" {
		t.Error("no summary stored; the fallback extract must be persisted")
	}
}

// Sanity guard on the fixture: the provider really does fail with a deadline
// error, so the test above exercises the fallback path and not the happy path.
func TestDeadlineBurningProviderFailsWithContextError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := (&deadlineBurningProvider{}).Chat(ctx, providers.ChatRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("provider error = %v, want context.DeadlineExceeded", err)
	}
}
