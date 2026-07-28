package cmd

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func strPtr(s string) *string { return &s }

// TestDeterministicWorkflowSummaryRedactsInternalFailureText proves a partially
// failed workflow tells the requester the step did not finish WITHOUT pasting the
// operator's diagnostic string into their message.
//
// Observed live (session test-route-multi-1785164106): the delivered message
// contained "FAILED: iter 1 think: llm call: provider returned a successful
// response with no content, no tool calls and no usage (9router/Huy-Minh)",
// leaking the upstream gateway name, the internal model alias, and agent loop
// internals to the end user.
func TestDeterministicWorkflowSummaryRedactsInternalFailureText(t *testing.T) {
	rawError := "FAILED: iter 1 think: llm call: provider returned a successful response with no content, no tool calls and no usage (9router/Huy-Minh)"
	workflow := &store.TeamWorkflowData{Status: store.TeamWorkflowStatusFailing}
	tasks := []store.TeamTaskData{
		{
			WorkflowKind: store.TeamWorkflowTaskKindWork,
			WorkflowStepID: "market-analysis",
			Subject:      "Phân tích thị trường",
			Status:       store.TeamTaskStatusCompleted,
			Result:       strPtr("Báo cáo phân tích thị trường đã hoàn thành với các phát hiện chính."),
		},
		{
			WorkflowKind: store.TeamWorkflowTaskKindWork,
			WorkflowStepID: "launch-strategy",
			Subject:      "Xây dựng chiến lược ra mắt",
			Status:       store.TeamTaskStatusFailed,
			Result:       strPtr(rawError),
		},
	}

	for _, locale := range []string{"vi", "en"} {
		summary := deterministicWorkflowSummary(workflow, tasks, locale)

		for _, leak := range []string{"9router", "Huy-Minh", "iter 1 think", "llm call", "FAILED:"} {
			if strings.Contains(summary, leak) {
				t.Errorf("locale %s: summary leaks internal detail %q:\n%s", locale, leak, summary)
			}
		}
		// The completed step's real deliverable must survive.
		if !strings.Contains(summary, "Báo cáo phân tích thị trường") {
			t.Errorf("locale %s: completed step result was dropped:\n%s", locale, summary)
		}
		// The user must still learn the step did not finish.
		if !strings.Contains(summary, "Xây dựng chiến lược ra mắt") {
			t.Errorf("locale %s: user was not told which step failed:\n%s", locale, summary)
		}
	}
}

// TestBuildWorkflowFinalizePromptRedactsInternalFailureText covers the other
// delivery path: when the LLM synthesizer runs, it must not be handed the raw
// error either, because it treats step results as material to preserve.
func TestBuildWorkflowFinalizePromptRedactsInternalFailureText(t *testing.T) {
	workflow := &store.TeamWorkflowData{Status: store.TeamWorkflowStatusFailing}
	tasks := []store.TeamTaskData{{
		WorkflowKind:   store.TeamWorkflowTaskKindWork,
		WorkflowStepID: "launch-strategy",
		Subject:        "Xây dựng chiến lược ra mắt",
		Status:         store.TeamTaskStatusFailed,
		Result:         strPtr("FAILED: iter 1 think: llm call: provider returned no content (9router/Huy-Minh)"),
	}}
	prompt := buildWorkflowFinalizePrompt(workflow, tasks)
	for _, leak := range []string{"9router", "Huy-Minh", "iter 1 think", "FAILED:"} {
		if strings.Contains(prompt, leak) {
			t.Errorf("finalize prompt leaks internal detail %q:\n%s", leak, prompt)
		}
	}
}
