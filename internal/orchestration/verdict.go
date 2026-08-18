package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/nextlevelbuilder/goclaw/internal/contract"
)

// Scorer evaluates a single contender outcome. It returns a score in [0, 1]
// (higher is better) and a human-readable justification. The label lets a
// single scoring function vary its rubric by strategy, e.g. "simplest".
type Scorer func(result ChildResult, label string) (score float64, reason string)

// JudgeOpts configures a judge round.
type JudgeOpts struct {
	// Criteria lists the scoring dimensions used to rank contenders. Repetition
	// of a criterion adds weight, e.g. {"correctness", "correctness",
	// "performance"} weighs correctness 2x performance.
	Criteria []string
	// Scoring maps a criterion name to its scorer. Criteria with no scorer in
	// the map are ignored.
	Scoring map[string]Scorer
	// Match is the approval threshold: a contender whose weighted score is
	// >= Match is approved, >= Match*0.8 is "revise", otherwise "reject".
	// Zero defaults to 0.66 (2/3).
	Match float64
}

// Judge decisions emitted by Judge.
const (
	judgeApprove = "approve"
	judgeRevise  = "revise"
	judgeReject  = "reject"
)

// defaultMatch is the approval threshold used when JudgeOpts.Match is unset.
const defaultMatch = 0.66

// reviseRatio sits between the approve threshold and zero; below it a
// contender is rejected outright.
const reviseRatio = 0.8

// ErrNoResults reports a judge round with no results to evaluate.
var ErrNoResults = errors.New("orchestration: Judge: no results")

// ErrNoScoring reports a judge round with no usable scoring criteria.
var ErrNoScoring = errors.New("orchestration: Judge: no scoring criteria or scorers")

// scoreCard holds a contender's accumulated weighted score.
type scoreCard struct {
	contenderID string
	label       string
	weighted    float64
	reasons     []string
	hasScore    bool
}

// Judge ranks the contenders by their weighted score over the configured
// criteria and returns the verdict for the best one. A contender with no
// score on any criterion is ineligible for approval. The returned verdict's
// ContenderID names the winner; when multiple results share the top score the
// first in contestant order wins. The outcome and final ranking are logged via
// slog so operators can trace judge decisions.
func Judge(ctx context.Context, contestants []Contestant, results []ChildResult, opts JudgeOpts) (contract.Verdict, error) {
	if ctx == nil {
		return contract.Verdict{}, errors.New("orchestration: Judge: nil context")
	}
	if len(contestants) == 0 || len(results) == 0 {
		return contract.Verdict{}, ErrNoResults
	}
	if len(contestants) != len(results) {
		return contract.Verdict{}, fmt.Errorf("orchestration: Judge: %d contestants but %d results", len(contestants), len(results))
	}

	// Collect usable criteria (those with a scorer in the map). Repetition in
	// the input list is preserved so repeated criteria accumulate weight.
	criteria := make([]string, 0, len(opts.Criteria))
	for _, c := range opts.Criteria {
		if opts.Scoring[c] != nil {
			criteria = append(criteria, c)
		}
	}
	if len(criteria) == 0 {
		return contract.Verdict{}, ErrNoScoring
	}

	match := opts.Match
	if match <= 0 || match > 1 {
		match = defaultMatch
	}

	cards := make([]scoreCard, len(contestants))
	for i, c := range contestants {
		cards[i].contenderID = c.ID
		cards[i].label = c.Label
		for _, crit := range criteria {
			if results[i].Status == "failed" {
				// A failed contender earns nothing on any criterion.
				continue
			}
			score, reason := opts.Scoring[crit](results[i], c.Label)
			if score < 0 {
				score = 0
			}
			if score > 1 {
				score = 1
			}
			cards[i].weighted += score
			cards[i].reasons = append(cards[i].reasons, reason)
			cards[i].hasScore = true
		}
	}

	// Normalize by the number of scoring passes so the result stays in [0,1].
	passes := len(criteria)
	for i := range cards {
		if cards[i].hasScore {
			cards[i].weighted /= float64(passes)
		}
	}

	// Best card by weighted score, then by contestant order for stable ties.
	best := -1
	for i := range cards {
		if cards[i].hasScore && (best < 0 || cards[i].weighted > cards[best].weighted) {
			best = i
		}
	}
	if best < 0 {
		// Every contender failed or scored nothing.
		v := contract.Verdict{ContenderID: firstNonEmptyID(contestants), Decision: judgeReject, Reason: "no contender produced a scorable result"}
		slog.Warn("orchestration.judge", "decision", v.Decision, "contender", v.ContenderID, "no_score", true)
		return v, nil
	}

	top := cards[best]
	decision := judgeReject
	switch {
	case top.weighted >= match:
		decision = judgeApprove
	case top.weighted >= match*reviseRatio:
		decision = judgeRevise
	}

	ranked := append([]scoreCard(nil), cards...)
	sort.SliceStable(ranked, func(a, b int) bool { return ranked[a].weighted > ranked[b].weighted })
	logVerdict(ranked, top, decision, match)

	return contract.Verdict{
		ContenderID: top.contenderID,
		Decision:    decision,
		Score:       roundScore(top.weighted),
		Reason:      joinReasons(top.reasons),
		Votes:       1, // a single judge; jury aggregation adds further verdicts
	}, nil
}

// firstNonEmptyID returns the first non-empty contestant ID, or "".
func firstNonEmptyID(contestants []Contestant) string {
	for _, c := range contestants {
		if c.ID != "" {
			return c.ID
		}
	}
	return ""
}

// joinReasons concatenates unique scoring reasons into one summary string.
func joinReasons(reasons []string) string {
	sep := ""
	out := ""
	seen := make(map[string]bool, len(reasons))
	for _, r := range reasons {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out += sep + r
		sep = "; "
	}
	return out
}

// roundScore snaps a score to 3 decimals to keep verdict serialization clean.
func roundScore(s float64) float64 {
	return float64(int64(s*1000+0.5)) / 1000
}

// logVerdict emits the winner and the ranked field under a single slog record.
func logVerdict(ranked []scoreCard, top scoreCard, decision string, match float64) {
	fields := []any{
		"decision", decision,
		"winner", top.contenderID,
		"score", roundScore(top.weighted),
		"threshold", roundScore(match),
	}
	for i, c := range ranked {
		fields = append(fields, fmt.Sprintf("rank_%d", i+1), c.contenderID+":"+roundScoreStr(c.weighted))
	}
	slog.Info("orchestration.judge", fields...)
}

// roundScoreStr formats a score for the log line.
func roundScoreStr(s float64) string {
	return fmt.Sprintf("%.3f", s)
}