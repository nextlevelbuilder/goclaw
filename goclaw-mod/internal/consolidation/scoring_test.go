package consolidation

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// floatEq checks two floats are equal within epsilon for the scoring assertions.
func floatEq(a, b, eps float64) bool { return math.Abs(a-b) < eps }

func TestComputeRecallScoreZeroRecallFreshEntry(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entry := store.EpisodicSummary{
		ID:        uuid.New(),
		CreatedAt: now, // brand new, zero recalls
	}
	// freq=0, rel=0, recency=1.0, freshness=1.0
	// 0.30*0 + 0.35*0 + 0.20*1 + 0.15*1 = 0.35
	score := ComputeRecallScore(entry, now)
	if !floatEq(score, 0.35, 0.001) {
		t.Errorf("score = %f, want ~0.35", score)
	}
}

func TestComputeRecallScoreOldUnrecalled(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entry := store.EpisodicSummary{
		CreatedAt: now.AddDate(0, 0, -28), // 28 days old, never recalled
	}
	// recency = freshness = exp(-ln2/14 * 28) = exp(-ln2*2) = 0.25
	// score = 0 + 0 + 0.2*0.25 + 0.15*0.25 = 0.0875
	score := ComputeRecallScore(entry, now)
	if !floatEq(score, 0.0875, 0.001) {
		t.Errorf("score = %f, want ~0.0875", score)
	}
}

func TestComputeRecallScoreExponentialDecayHalfLife(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	// Entry recalled exactly 14 days ago (one half-life). Recency should halve.
	lastRecall := now.AddDate(0, 0, -14)
	entry := store.EpisodicSummary{
		CreatedAt:      lastRecall, // created at same moment it was last recalled
		RecallCount:    1,
		RecallScore:    1.0,
		LastRecalledAt: &lastRecall,
	}
	// freq = log1p(1) / log1p(10) = 0.693/2.398 ≈ 0.289
	// rel = 1.0
	// recency = freshness = exp(-ln2) = 0.5
	// score = 0.30*0.289 + 0.35*1.0 + 0.20*0.5 + 0.15*0.5 = 0.0867 + 0.35 + 0.175 = 0.6117
	score := ComputeRecallScore(entry, now)
	if !floatEq(score, 0.6117, 0.001) {
		t.Errorf("score = %f, want ~0.6117", score)
	}
}

func TestComputeRecallScoreHighRecallSignal(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entry := store.EpisodicSummary{
		CreatedAt:      now.AddDate(0, 0, -3), // 3 days old
		RecallCount:    10,
		RecallScore:    0.9,
		LastRecalledAt: &now, // recalled today
	}
	// freq ≈ 1.0 (cap)
	// rel = 0.9
	// recency ≈ 1.0 (lastRef=now)
	// freshness = exp(-ln2/14 * 3) ≈ 0.862
	// score ≈ 0.30 + 0.315 + 0.20 + 0.1293 ≈ 0.944
	score := ComputeRecallScore(entry, now)
	if score < 0.9 {
		t.Errorf("score = %f, want >0.9 (hot entry)", score)
	}
}

func TestComputeRecallScoreClampsRelevance(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	// Malformed entry with out-of-range recall_score — scoring must clamp.
	entry := store.EpisodicSummary{
		CreatedAt:   now,
		RecallCount: 1,
		RecallScore: 2.5, // bug: > 1.0
	}
	score := ComputeRecallScore(entry, now)
	if score > 1.1 {
		t.Errorf("score = %f exceeds reasonable bounds (relevance should clamp)", score)
	}
}

func TestFilterByRecallThresholdsFreshBypass(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entries := []store.EpisodicSummary{
		// Never recalled, 28 days old → low score but bypasses via RecallCount=0.
		{ID: uuid.New(), CreatedAt: now.AddDate(0, 0, -28)},
	}
	kept := filterByRecallThresholds(entries, recallThresholds{MinScore: 0.5, MinRecallCount: 2}, now)
	if len(kept) != 1 {
		t.Errorf("expected 1 kept (fresh bypass), got %d", len(kept))
	}
}

func TestFilterByRecallThresholdsPreservesOrder(t *testing.T) {
	// In-place filter must not scramble the caller's ORDER BY recall_score DESC.
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entries := []store.EpisodicSummary{
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), CreatedAt: now, RecallCount: 10, RecallScore: 0.9, LastRecalledAt: &now},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), CreatedAt: now, RecallCount: 1, RecallScore: 0.1}, // filtered
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), CreatedAt: now, RecallCount: 5, RecallScore: 0.7, LastRecalledAt: &now},
	}
	kept := filterByRecallThresholds(entries, defaultRecallThresholds(), now)
	if len(kept) != 2 {
		t.Fatalf("len=%d, want 2", len(kept))
	}
	if kept[0].ID.String() != "00000000-0000-0000-0000-000000000001" ||
		kept[1].ID.String() != "00000000-0000-0000-0000-000000000003" {
		t.Errorf("order corrupted: %v", []string{kept[0].ID.String(), kept[1].ID.String()})
	}
}

func TestFilterByRecallThresholdsDropsWeakRecalled(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entries := []store.EpisodicSummary{
		// Recalled once — below MinRecallCount=2 → filtered.
		{ID: uuid.New(), CreatedAt: now, RecallCount: 1, RecallScore: 0.9},
		// Recalled 3 times with good score — kept.
		{ID: uuid.New(), CreatedAt: now, RecallCount: 3, RecallScore: 0.8, LastRecalledAt: &now},
		// Never recalled — kept via freshness.
		{ID: uuid.New(), CreatedAt: now},
	}
	kept := filterByRecallThresholds(entries, defaultRecallThresholds(), now)
	if len(kept) != 2 {
		t.Errorf("expected 2 kept, got %d", len(kept))
	}
}

func TestFormatEntryForSynthesisAnnotatesRecalled(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	entry := store.EpisodicSummary{
		Summary:        "test session",
		RecallCount:    5,
		LastRecalledAt: &now,
	}
	out := formatEntryForSynthesis(entry)
	if !strings.Contains(out, "recalled 5x") || !strings.Contains(out, "test session") {
		t.Errorf("output missing metadata or summary: %q", out)
	}
}

func TestFormatEntryForSynthesisBypassesUnrecalled(t *testing.T) {
	entry := store.EpisodicSummary{Summary: "fresh content"}
	if out := formatEntryForSynthesis(entry); out != "fresh content" {
		t.Errorf("unrecalled entry should pass through unchanged, got %q", out)
	}
}
