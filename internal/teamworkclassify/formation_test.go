package teamworkclassify

import (
	"errors"
	"strings"
	"testing"
)

func TestSelectFormation_ExplicitOverrideWins(t *testing.T) {
	f, err := SelectFormation("some task", "low", "architect-review-team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != FormationArchitectReview {
		t.Fatalf("override did not win: got formation %q, want %q", f.Name, FormationArchitectReview)
	}
	if !equalStrings(f.Agents, []string{"arch", "review"}) {
		t.Fatalf("unexpected agents for architect-review-team: %v", f.Agents)
	}
	if !equalStrings(f.Pipeline, []string{"design", "review"}) {
		t.Fatalf("unexpected pipeline for architect-review-team: %v", f.Pipeline)
	}
	// Base mode string is unchanged by formation routing.
	if got := ModeFormation(f); got != "formation:architect-review-team" {
		t.Fatalf("ModeFormation = %q, want %q", got, "formation:architect-review-team")
	}
}

func TestSelectFormation_OverrideCaseInsensitiveAndTrimmed(t *testing.T) {
	f, err := SelectFormation("task", "", "  Planner-Coder-Tester  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != FormationPlannerCoderTester {
		t.Fatalf("got formation %q, want %q", f.Name, FormationPlannerCoderTester)
	}
}

func TestSelectFormation_UnknownOverrideErrors(t *testing.T) {
	_, err := SelectFormation("task", "low", "no-such-formation")
	if !errors.Is(err, ErrUnknownFormation) {
		t.Fatalf("expected ErrUnknownFormation, got %v", err)
	}
}

func TestSelectFormation_DefaultForEmptyTaskAndComplexity(t *testing.T) {
	f, err := SelectFormation("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != FormationSoloFollowup {
		t.Fatalf("empty input should route to solo-followup, got %q", f.Name)
	}
	if got := FormationCategory(f.Name); got != FormationCategorySolo {
		t.Fatalf("category = %q, want %q", got, FormationCategorySolo)
	}
}

func TestSelectFormation_DeterministicByComplexity(t *testing.T) {
	cases := []struct {
		complexity string
		want       string
	}{
		{"high", FormationArchitectReview},
		{"complex", FormationArchitectReview},
		{"medium", FormationDebuggerPanel},
		{"moderate", FormationDebuggerPanel},
		{"build", FormationPlannerCoderTester},
		{"feature", FormationPlannerCoderTester},
		{"low", FormationSoloFollowup},
		{"", FormationSoloFollowup},
		{"unknown-string", FormationSoloFollowup},
	}
	for _, tc := range cases {
		t.Run(tc.complexity, func(t *testing.T) {
			f, err := SelectFormation("task", tc.complexity, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.Name != tc.want {
				t.Fatalf("complexity %q routed to %q, want %q", tc.complexity, f.Name, tc.want)
			}
			// Deterministic: calling twice with the same input must be stable.
			f2, err := SelectFormation("task", tc.complexity, "")
			if err != nil {
				t.Fatalf("unexpected error on second call: %v", err)
			}
			if f2.Name != f.Name || f2.Complexity != f.Complexity {
				t.Fatalf("formation not deterministic: %+v vs %+v", f, f2)
			}
		})
	}
}

func TestFormationModeFor_ExtendsModeAdditively(t *testing.T) {
	f, err := SelectFormation("task", "medium", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fm := FormationModeFor(ModeTeam, f)
	wantMode := string(ModeTeam) + ":formation:debugger-panel"
	if fm.Mode != wantMode {
		t.Fatalf("FormationModeFor.Mode = %q, want %q", fm.Mode, wantMode)
	}
	if fm.Category != FormationCategoryDebugger {
		t.Fatalf("FormationModeFor.Category = %q, want %q", fm.Category, FormationCategoryDebugger)
	}
}

func TestFormationCategory_UnknownFallsBackToSolo(t *testing.T) {
	if got := FormationCategory("does-not-exist"); got != FormationCategorySolo {
		t.Fatalf("unknown formation category = %q, want %q", got, FormationCategorySolo)
	}
}

func TestSelectFormation_OverrideDoesNotChangeCatalog(t *testing.T) {
	// The catalog is read-only: selecting an override must not mutate the
	// shared map so later plain-complexity selections stay unchanged.
	_, _ = SelectFormation("t", "", "architect-review-team")
	f, err := SelectFormation("t", "high", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != FormationArchitectReview {
		t.Fatalf("high complexity should still route to architect-review-team, got %q", f.Name)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}