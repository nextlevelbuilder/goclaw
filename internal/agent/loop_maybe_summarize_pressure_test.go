package agent

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Fixture math for these tests (contextWindow=200000, maxTokens=8192, nil cfg):
//
//	effectiveMaxTokens   = 8192
//	compactionInputCap   = min(200000-8192, int(200000*0.85)-8192) = min(191808, 161808) = 161808
//	baseline threshold   = int(200000*0.85)                        = 170000
//	overhead (no calib)  = min(int(200000*0.2), 40000)             = 40000
//	pressureThreshold    = max(161808-40000, 0)                    = 121808
//	defensive floor      = 170000/2                                = 85000  (< 121808, so unused)
//
// Token estimate (no calibration) = sum(runes(content)/3) over all history messages.
// Bands used below:
//
//	~100k tokens → below BOTH thresholds → skip even under mid-loop pressure
//	~150k tokens → between pressure(121808) and baseline(170000) → persist ONLY under pressure
const (
	pressureContextWindow = 200000
	pressureMaxTokens     = 8192
)

// buildTokenHistory returns n alternating user/assistant messages whose combined
// rune/3 token estimate is approximately targetTokens. Each message carries
// targetTokens/n * 3 runes so EstimateTokens(history) ≈ targetTokens.
func buildTokenHistory(n, targetTokens int) []providers.Message {
	perMsgTokens := targetTokens / n
	perMsgRunes := perMsgTokens * 3
	content := makeLongString(perMsgRunes)
	history := make([]providers.Message, n)
	for i := range history {
		if i%2 == 0 {
			history[i] = providers.Message{Role: "user", Content: content}
		} else {
			history[i] = providers.Message{Role: "assistant", Content: content}
		}
	}
	return history
}

// waitForCount polls get() until it reaches want or the deadline elapses.
func waitForCount(t *testing.T, get func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if get() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for count to reach %d, last=%d", want, get())
}

func newPressureLoop(sessions *nopSessionStore, provider providers.Provider) *Loop {
	return &Loop{
		id:            "test-agent",
		provider:      provider,
		model:         "claude-3-5-sonnet",
		contextWindow: pressureContextWindow,
		maxTokens:     pressureMaxTokens,
		sessions:      sessions,
		hasMemory:     false,
		compactionCfg: nil,
	}
}

// V1-B / V2 core: a run that compacted mid-loop lowers the threshold to
// pressureThreshold (121808). History at ~150k tokens is BELOW the baseline
// (170000) but ABOVE the pressure threshold → summarize must run and PERSIST
// (TruncateHistory + IncrementCompaction each exactly once).
func TestMaybeSummarize_MidLoopPressurePersists(t *testing.T) {
	sessions := &nopSessionStore{
		history: buildTokenHistory(20, 150000), // ~150k tokens
	}
	loop := newPressureLoop(sessions, &capturingProvider{response: "compaction summary"})

	loop.maybeSummarize(context.Background(), "sess-1", true)

	waitForCount(t, sessions.incrementCount, 1)

	if got := sessions.incrementCount(); got != 1 {
		t.Errorf("IncrementCompaction called %d times, want 1", got)
	}
	if got := sessions.truncateCount(); got != 1 {
		t.Errorf("TruncateHistory called %d times, want 1", got)
	}
	sessions.mu.Lock()
	keepLast := sessions.truncateKeepLast
	setSummary := sessions.setSummaryCalls
	sessions.mu.Unlock()
	if keepLast != 4 {
		t.Errorf("TruncateHistory keepLast = %d, want 4", keepLast)
	}
	if setSummary != 1 {
		t.Errorf("SetSummary called %d times, want 1", setSummary)
	}
}

// V2 no-over-compaction (baseline case): the SAME ~150k history without mid-loop
// pressure must SKIP (150000 < 170000 baseline). No summarize, no persist.
func TestMaybeSummarize_NonMidLoopSkipsUnderBaseline(t *testing.T) {
	provider := &capturingProvider{response: "should not be called"}
	sessions := &nopSessionStore{
		history: buildTokenHistory(20, 150000), // ~150k tokens
	}
	loop := newPressureLoop(sessions, provider)

	loop.maybeSummarize(context.Background(), "sess-1", false)

	// Skip returns synchronously before spawning the goroutine; give any
	// (erroneously spawned) goroutine a brief window to prove it didn't run.
	time.Sleep(50 * time.Millisecond)

	if got := len(provider.captured); got != 0 {
		t.Errorf("provider.Chat called %d times, want 0 (under baseline, no pressure)", got)
	}
	if got := sessions.incrementCount(); got != 0 {
		t.Errorf("IncrementCompaction called %d times, want 0", got)
	}
	if got := sessions.truncateCount(); got != 0 {
		t.Errorf("TruncateHistory called %d times, want 0", got)
	}
}

// V2 no-over-compaction (the KEY case that distinguishes "lower threshold" from
// "skip threshold"): mid-loop pressure is set, but the real history is small
// (~100k tokens, below pressureThreshold=121808). The mid-loop compaction was
// driven by transient tool-result bloat, not history — so we must STILL SKIP and
// NOT truncate the session. This proves we lowered the threshold rather than
// bypassing it.
func TestMaybeSummarize_MidLoopSkipsWhenHistorySmall(t *testing.T) {
	provider := &capturingProvider{response: "should not be called"}
	sessions := &nopSessionStore{
		history: buildTokenHistory(20, 100000), // ~100k tokens < pressureThreshold
	}
	loop := newPressureLoop(sessions, provider)

	loop.maybeSummarize(context.Background(), "sess-1", true)

	time.Sleep(50 * time.Millisecond)

	if got := len(provider.captured); got != 0 {
		t.Errorf("provider.Chat called %d times, want 0 (history below pressure threshold)", got)
	}
	if got := sessions.incrementCount(); got != 0 {
		t.Errorf("IncrementCompaction called %d times, want 0 (no over-compaction)", got)
	}
	if got := sessions.truncateCount(); got != 0 {
		t.Errorf("TruncateHistory called %d times, want 0 (no over-compaction)", got)
	}
}

// V2 threshold-sync boundary: with one fixed history between the pressure
// threshold (121808) and the baseline (170000), the ONLY thing that flips the
// decision is the mid-loop flag. Persist when true, skip when false — proving
// the two thresholds bracket this history exactly as designed.
func TestMaybeSummarize_ThresholdSyncBoundary(t *testing.T) {
	history := buildTokenHistory(20, 150000) // between 121808 and 170000

	// mid-loop = false → skip
	skipProvider := &capturingProvider{response: "unused"}
	skipSessions := &nopSessionStore{history: cloneMessages(history)}
	newPressureLoop(skipSessions, skipProvider).maybeSummarize(context.Background(), "sess-1", false)
	time.Sleep(50 * time.Millisecond)
	if got := skipSessions.incrementCount(); got != 0 {
		t.Errorf("non-mid-loop: IncrementCompaction = %d, want 0 (skip above pressure, below baseline)", got)
	}

	// mid-loop = true → persist
	triggerSessions := &nopSessionStore{history: cloneMessages(history)}
	newPressureLoop(triggerSessions, &capturingProvider{response: "compaction summary"}).
		maybeSummarize(context.Background(), "sess-1", true)
	waitForCount(t, triggerSessions.incrementCount, 1)
	if got := triggerSessions.incrementCount(); got != 1 {
		t.Errorf("mid-loop: IncrementCompaction = %d, want 1 (persist under pressure)", got)
	}
}

// V2 anti-loop (characterization): turn 1 persists the mid-loop compaction,
// shrinking the session history. Turn 2 loads the now-small history, so even
// with mid-loop pressure still set it stays below the pressure threshold and
// does NOT re-compact — the unbounded re-compaction loop is broken.
func TestMaybeSummarize_AntiLoop_SecondTurnDoesNotRecompact(t *testing.T) {
	sessions := &nopSessionStore{
		history: buildTokenHistory(20, 150000),
	}
	loop := newPressureLoop(sessions, &capturingProvider{response: "compaction summary"})

	// Turn 1: pressure present, history large → persist (truncate to keepLast=4).
	loop.maybeSummarize(context.Background(), "sess-1", true)
	waitForCount(t, sessions.incrementCount, 1)

	sessions.mu.Lock()
	remaining := len(sessions.history)
	sessions.mu.Unlock()
	if remaining > 4 {
		t.Fatalf("after turn 1 persist, history len = %d, want <= 4 (keepLast)", remaining)
	}

	// Turn 2: history is now tiny (4 messages). Even with pressure set, the
	// history-only estimate is far below pressureThreshold → skip.
	loop.maybeSummarize(context.Background(), "sess-1", true)
	time.Sleep(50 * time.Millisecond)

	if got := sessions.incrementCount(); got != 1 {
		t.Errorf("after turn 2, IncrementCompaction total = %d, want 1 (no re-compaction)", got)
	}
	if got := sessions.truncateCount(); got != 1 {
		t.Errorf("after turn 2, TruncateHistory total = %d, want 1 (no re-compaction)", got)
	}
}

// Media preservation regression: MediaRefs on messages about to be truncated
// must be carried onto the first kept message so the agent doesn't lose track of
// shared files across a pressure-driven compaction.
func TestMaybeSummarize_MidLoopPreservesMediaRefs(t *testing.T) {
	history := buildTokenHistory(20, 150000)
	// Attach a media ref to an early message (will be in the to-summarize slice).
	history[0].MediaRefs = []providers.MediaRef{
		{ID: "img-1", MimeType: "image/png", Kind: "image", Path: "/tmp/img-1.png"},
	}
	sessions := &nopSessionStore{history: history}
	loop := newPressureLoop(sessions, &capturingProvider{response: "compaction summary"})

	loop.maybeSummarize(context.Background(), "sess-1", true)
	waitForCount(t, sessions.incrementCount, 1)

	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.history) == 0 {
		t.Fatal("history empty after truncation")
	}
	var found bool
	for _, ref := range sessions.history[0].MediaRefs {
		if ref.ID == "img-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("preserved media ref img-1 not found on first kept message; refs=%+v", sessions.history[0].MediaRefs)
	}
}

// cloneMessages returns a shallow copy of a message slice so two Loops don't
// share the same backing array (TruncateHistory mutates it in the mock).
func cloneMessages(msgs []providers.Message) []providers.Message {
	out := make([]providers.Message, len(msgs))
	copy(out, msgs)
	return out
}
