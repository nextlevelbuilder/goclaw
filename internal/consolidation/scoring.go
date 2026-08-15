package consolidation

import (
	"math"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Scoring constants. Chosen to match the plan's 4-component formula (plan has
// 6 components but GoClaw lacks per-episode conceptual + consolidation data).
//
//	score = 0.30*frequency + 0.35*relevance + 0.20*recency + 0.15*freshness
//
// halfLifeDays=14 matches typical short-term memory retention windows; tuning
// should be informed by operator feedback once real recall data accumulates.
const (
	recallWeightFrequency = 0.30
	recallWeightRelevance = 0.35
	recallWeightRecency   = 0.20
	recallWeightFreshness = 0.15
	recallHalfLifeDays    = 14.0
	// frequencyCap: recall_count above this yields max frequency component
	// (avoids runaway-frequency bias for hot entries).
	recallFrequencyCap = 10
)

// recallThresholds holds the minimum scoring cutoffs applied to
// ListUnpromotedScored results before LLM synthesis. Chosen well below the
// TS reference (0.75 / 3) because GoClaw boots with zero recall data —
// early-life agents must still reach synthesis via the freshness component.
type recallThresholds struct {
	MinScore       float64
	MinRecallCount int
}

func defaultRecallThresholds() recallThresholds {
	return recallThresholds{MinScore: 0.2, MinRecallCount: 2}
}

// ComputeRecallScore computes the weighted recall score for an episodic entry.
// Pure function — no IO, no randomness — so the dreaming worker can compute
// scores on demand instead of materialising them into the DB on every event.
//
// Returned value is in [0, 1+ε]. In practice results cluster in [0, 0.8];
// thresholds in defaultRecallThresholds are calibrated to that range.
func ComputeRecallScore(entry store.EpisodicSummary, now time.Time) float64 {
	// 1. Frequency (30%): log-normalised recall count. Log1p smooths the
	// curve so 0 → 0, 1 → 0.29, 10 → 1.0; anything above cap stays at 1.
	freq := math.Log1p(float64(entry.RecallCount)) / math.Log1p(recallFrequencyCap)
	if freq > 1.0 {
		freq = 1.0
	}

	// 2. Relevance (35%): running average of memory_search hit scores.
	// Already lives in recall_score column and is clamped server-side to [0,1]
	// by RecordRecall callers. Defensive clamp here for robustness.
	rel := entry.RecallScore
	if rel < 0 {
		rel = 0
	} else if rel > 1 {
		rel = 1
	}

	// Exponential decay shared by recency + freshness components.
	// lambda = ln(2) / halfLife → score halves every `recallHalfLifeDays` days.
	lambda := math.Ln2 / recallHalfLifeDays

	// 3. Recency (20%): decay from LastRecalledAt (falls back to CreatedAt
	// for entries that have never been searched). Entries recalled today
	// score ~1.0; 14 days ago → 0.5; 28 days ago → 0.25.
	lastRef := entry.CreatedAt
	if entry.LastRecalledAt != nil {
		lastRef = *entry.LastRecalledAt
	}
	recency := decayFrom(lastRef, now, lambda)

	// 4. Freshness (15%): decay from CreatedAt regardless of recall history.
	// Keeps brand-new entries eligible even with zero recall — prevents cold
	// start from starving the synthesis pipeline.
	freshness := decayFrom(entry.CreatedAt, now, lambda)

	return recallWeightFrequency*freq +
		recallWeightRelevance*rel +
		recallWeightRecency*recency +
		recallWeightFreshness*freshness
}

// decayFrom returns exp(-lambda * ageDays). Negative ages (clock skew,
// future-dated rows) clamp to 1.0 so the test fixtures are predictable.
func decayFrom(then, now time.Time, lambda float64) float64 {
	if then.IsZero() || !then.Before(now) {
		return 1.0
	}
	ageDays := now.Sub(then).Hours() / 24
	return math.Exp(-lambda * ageDays)
}

// filterByRecallThresholds drops entries whose recall signal is too weak
// to justify LLM synthesis. Pure function so the dreaming worker can
// sort-then-filter in a single pass.
//
// Bootstrap-friendly rules:
//   - Never-recalled entries (RecallCount == 0) bypass BOTH thresholds.
//     Freshness + recency keep them eligible so new agents can synthesise
//     their earliest sessions without any memory_search activity.
//   - Entries that HAVE been recalled must clear both MinRecallCount and
//     MinScore. If an entry has seen 1 weak hit, it's filtered.
func filterByRecallThresholds(entries []store.EpisodicSummary, th recallThresholds, now time.Time) []store.EpisodicSummary {
	if len(entries) == 0 {
		return entries
	}
	kept := entries[:0]
	for _, e := range entries {
		if e.RecallCount == 0 {
			kept = append(kept, e)
			continue
		}
		if e.RecallCount < th.MinRecallCount {
			continue
		}
		if ComputeRecallScore(e, now) < th.MinScore {
			continue
		}
		kept = append(kept, e)
	}
	return kept
}
