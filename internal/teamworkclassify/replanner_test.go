package teamworkclassify

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPlanWorkflowReplacementFailsClosed(t *testing.T) {
	input := plannerTestInput()
	input.Message = "Replace the blocked workflow"

	tests := []struct {
		name     string
		provider *fakeArbiterProvider
	}{
		{name: "provider error", provider: &fakeArbiterProvider{err: errors.New("unavailable")}},
		{name: "nil response content", provider: &fakeArbiterProvider{content: ""}},
		{name: "self response", provider: &fakeArbiterProvider{content: arbiterJSON("self", "strong", "none", "self_work", "", "do it directly", true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := PlanWorkflowReplacement(context.Background(), input, tt.provider, "test-model", nil, ReplanOptions{})
			if err == nil {
				t.Fatalf("PlanWorkflowReplacement() result = %+v, want fail-closed error", result)
			}
			if result.Decision != "" || result.Plan != nil {
				t.Fatalf("failure returned usable result: %+v", result)
			}
		})
	}
}

func TestBuildWorkflowReplanMessagesRequiresMultiRoleAndInheritedReview(t *testing.T) {
	input := plannerTestInput()
	messages := buildWorkflowReplanMessages(input, Evidence{}, WorkAssessment{WorkflowMode: WorkflowModeMultiRole}, true)
	joined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"backend workflow-recovery planner",
		`decision="team"`,
		`workflow_mode="multi_role"`,
		`required_tool="team_tasks"`,
		"Never return self",
		"stored validated plan requires independent review",
		// The inherited review requirement must still demand a critic, but stated as
		// the real invariant: the critic reviews ANOTHER agent's work and its result
		// reaches the terminal step.
		"critic step that reviews work owned by a DIFFERENT agent",
		"reaches the terminal step",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("replan prompt missing %q:\n%s", want, joined)
		}
	}
	// Replan must not re-teach the drafter==integrator shape the validator no
	// longer requires: a recovery plan for a team split into specialists has to be
	// free to use producer -> critic -> different integrator.
	for _, forbidden := range []string{
		"same owner terminal integration",
		"owner draft -> distinct critic",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("replan prompt still prescribes the rigid shape %q:\n%s", forbidden, joined)
		}
	}
}
