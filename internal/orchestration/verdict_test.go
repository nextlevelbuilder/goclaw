package orchestration

import (
	"context"
	"testing"
)

// correctnessScorer scores an outcome by whether its content equals the task.
func correctnessScorer(want string) Scorer {
	return func(res ChildResult, label string) (float64, string) {
		if res.Content == want {
			return 1.0, label + ": correct"
		}
		return 0.0, label + ": wrong"
	}
}

// lengthScorer scores short outputs higher ("simplest" criterion).
func lengthScorer(res ChildResult, label string) (float64, string) {
	n := len(res.Content)
	if n <= 4 {
		return 1.0, "short"
	}
	if n <= 8 {
		return 0.6, "medium"
	}
	return 0.2, "long"
}

func TestJudge_SelectsBestByCriteria(t *testing.T) {
	contestants := []Contestant{
		{ID: "a", Label: "simplest"},
		{ID: "b", Label: "performance"},
	}
	results := []ChildResult{
		{Content: "answer", Status: "completed"},
		{Content: "answer and more detail", Status: "completed"},
	}
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"correctness", "correctness"},
		Scoring:  map[string]Scorer{"correctness": correctnessScorer("answer")},
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.ContenderID != "a" {
		t.Errorf("winner = %q, want %q", v.ContenderID, "a")
	}
	if v.Decision != "approve" {
		t.Errorf("decision = %q, want approve", v.Decision)
	}
	if v.Score != 1.0 {
		t.Errorf("score = %v, want 1.0", v.Score)
	}
}

func TestJudge_CorrectnessDominatesSimplest(t *testing.T) {
	// Contender "b" is long but correct (score 1.0 with weights 2x correctness
	// + 1x simplicity = weighted 2/3); contender "a" is short but wrong
	// (weighted 1/3). Correctness must win.
	contestants := []Contestant{
		{ID: "a", Label: "simplest"},
		{ID: "b", Label: "performance"},
	}
	results := []ChildResult{
		{Content: "wrong", Status: "completed"},
		{Content: "correct answer here", Status: "completed"},
	}
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"correctness", "correctness", "complexity"},
		Scoring: map[string]Scorer{
			"correctness": correctnessScorer("correct answer here"),
			"complexity":  lengthScorer,
		},
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.ContenderID != "b" {
		t.Errorf("winner = %q, want %q", v.ContenderID, "b")
	}
	if v.Score < 0.7 {
		t.Errorf("score = %v, want >= 0.7 (correctness-weighted)", v.Score)
	}
}

func TestJudge_ReviseForCloseScore(t *testing.T) {
	contestants := []Contestant{{ID: "a"}, {ID: "b"}}
	results := []ChildResult{
		{Content: "everything", Status: "completed"},
		{Content: "short", Status: "completed"},
	}
	// score(a) = 0.2 (long), score(b) = 0.6 (medium). Best = b at 0.6.
	// With Match 0.66 the revise floor is 0.66*0.8 = 0.528, so 0.6 -> revise.
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"complexity"},
		Scoring:  map[string]Scorer{"complexity": lengthScorer},
		Match:    0.66,
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.ContenderID != "b" {
		t.Errorf("winner = %q, want b", v.ContenderID)
	}
	if v.Decision != "revise" {
		t.Errorf("decision = %q, want revise (score 0.6 between floor 0.528 and threshold 0.66)", v.Decision)
	}
}

func TestJudge_RejectBelowReviseFloor(t *testing.T) {
	contestants := []Contestant{{ID: "a"}, {ID: "b"}}
	results := []ChildResult{
		{Content: "shortest", Status: "completed"},
		{Content: "ok", Status: "completed"},
	}
	// Best = b, score 1.0, so not reject. To force reject, make both long.
	results[0] = ChildResult{Content: "a very long solution indeed", Status: "completed"}
	results[1] = ChildResult{Content: "also quite long and verbose", Status: "completed"}
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"complexity"},
		Scoring:  map[string]Scorer{"complexity": lengthScorer},
		Match:    0.8, // floor = 0.64; both score 0.2 -> reject
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.Decision != "reject" {
		t.Errorf("decision = %q, want reject (score 0.2 below 0.64 floor)", v.Decision)
	}
	if v.Score != 0.2 {
		t.Errorf("score = %v, want 0.2", v.Score)
	}
}

func TestJudge_AllFailedRejects(t *testing.T) {
	contestants := []Contestant{{ID: "a"}, {ID: "b"}}
	results := []ChildResult{
		{Status: "failed"},
		{Status: "failed"},
	}
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"correctness"},
		Scoring:  map[string]Scorer{"correctness": correctnessScorer("x")},
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.Decision != "reject" {
		t.Errorf("decision = %q, want reject for all-failed", v.Decision)
	}
}

func TestJudge_Errors(t *testing.T) {
	contestants := []Contestant{{ID: "a"}}
	results := []ChildResult{{Status: "completed"}}

	if _, err := Judge(nil, contestants, results, JudgeOpts{}); err == nil {
		t.Error("nil context: expected error")
	}
	if _, err := Judge(context.Background(), nil, results, JudgeOpts{}); err != ErrNoResults {
		t.Errorf("no contestants: got %v, want ErrNoResults", err)
	}
	if _, err := Judge(context.Background(), contestants, nil, JudgeOpts{}); err != ErrNoResults {
		t.Errorf("no results: got %v, want ErrNoResults", err)
	}
	if _, err := Judge(context.Background(), contestants, []ChildResult{results[0], results[0]}, JudgeOpts{}); err == nil {
		t.Error("mismatched lengths: expected error")
	}
	if _, err := Judge(context.Background(), contestants, results, JudgeOpts{Criteria: []string{"bogus"}}); err != ErrNoScoring {
		t.Errorf("no scoring: got %v, want ErrNoScoring", err)
	}
	// Criteria that exist but map to no scorer => ErrNoScoring too.
	if _, err := Judge(context.Background(), contestants, results, JudgeOpts{Criteria: []string{"correctness"}, Scoring: map[string]Scorer{"other": correctnessScorer("x")}}); err != ErrNoScoring {
		t.Errorf("unmapped criteria: got %v, want ErrNoScoring", err)
	}
}

func TestJudge_DefaultMatchAndVotes(t *testing.T) {
	contestants := []Contestant{{ID: "a"}}
	results := []ChildResult{{Content: "yes", Status: "completed"}}
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"correctness"},
		Scoring:  map[string]Scorer{"correctness": correctnessScorer("yes")},
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.Votes != 1 {
		t.Errorf("votes = %d, want 1 (single judge)", v.Votes)
	}
	if v.Decision != "approve" {
		t.Errorf("decision = %q, want approve under default 0.66", v.Decision)
	}
	// Reason should join non-empty scoring notes.
	if v.Reason == "" {
		t.Error("reason empty; want at least one scoring note")
	}
}

func TestJudge_TieBreaksByContestantOrder(t *testing.T) {
	contestants := []Contestant{{ID: "first"}, {ID: "second"}}
	results := []ChildResult{
		{Content: "same", Status: "completed"},
		{Content: "same", Status: "completed"},
	}
	v, err := Judge(context.Background(), contestants, results, JudgeOpts{
		Criteria: []string{"correctness"},
		Scoring:  map[string]Scorer{"correctness": correctnessScorer("same")},
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.ContenderID != "first" {
		t.Errorf("winner = %q, want first on tie", v.ContenderID)
	}
}