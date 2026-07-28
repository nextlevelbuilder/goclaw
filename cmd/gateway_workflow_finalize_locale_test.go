package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// viPlan builds a canonical plan the way the planner does for a Vietnamese
// request: goal and step titles in the requester's language.
func viPlan(t *testing.T) json.RawMessage {
	t.Helper()
	plan := map[string]any{
		"goal": "Xây dựng bộ tài liệu nhất quán phục vụ ra mắt dịch vụ khám sức khỏe doanh nghiệp.",
		"steps": []map[string]any{
			{"id": "research-market-positioning", "title": "Phân tích thị trường và định vị"},
			{"id": "write-landing-page-copy", "title": "Viết nội dung landing page"},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return raw
}

func enPlan(t *testing.T) json.RawMessage {
	t.Helper()
	plan := map[string]any{
		"goal": "Produce a consistent launch document set for the corporate health screening service.",
		"steps": []map[string]any{
			{"id": "research-market-positioning", "title": "Research market and positioning"},
			{"id": "write-landing-page-copy", "title": "Write landing page copy"},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return raw
}

// A Vietnamese requester whose workflow partially failed used to receive an
// English notice with a Vietnamese step title spliced into it:
//
//	"The request was partially processed. Some steps did not complete:
//	 - Phân tích thị trường và định vị: did not complete due to a temporary system error."
//
// (Observed live 2026-07-28, workflow 019fa72c-273d-7a30-bd16-a0691de73795.)
//
// workflowLocale read origin_routing->>'locale', which is populated from inbound
// message metadata that no channel writes — so it always fell through to "en".
// The canonical plan is the language signal that is actually present.
func TestWorkflowLocaleFallsBackToCanonicalPlanLanguage(t *testing.T) {
	t.Parallel()
	workflow := &store.TeamWorkflowData{
		Status:        store.TeamWorkflowStatusFailing,
		OriginRouting: json.RawMessage(`{}`),
		CanonicalPlan: viPlan(t),
	}
	if got := workflowLocale(workflow); got != "vi" {
		t.Fatalf("workflowLocale = %q, want vi — a Vietnamese plan must not deliver an English notice", got)
	}
}

// The explicit routing key stays authoritative when a channel does set it: plan
// detection is a fallback, not an override.
func TestWorkflowLocalePrefersExplicitRoutingLocale(t *testing.T) {
	t.Parallel()
	workflow := &store.TeamWorkflowData{
		OriginRouting: json.RawMessage(`{"locale":"en"}`),
		CanonicalPlan: viPlan(t),
	}
	if got := workflowLocale(workflow); got != "en" {
		t.Fatalf("workflowLocale = %q, want en — explicit routing locale must win over detection", got)
	}
}

// An English plan must stay English, and a workflow with no plan at all (or an
// unparseable one) must not guess.
func TestWorkflowLocaleDefaultsToEnglish(t *testing.T) {
	t.Parallel()
	cases := map[string]json.RawMessage{
		"english plan":   enPlan(t),
		"empty plan":     nil,
		"invalid plan":   json.RawMessage(`{not json`),
		"plan no fields": json.RawMessage(`{}`),
	}
	for name, plan := range cases {
		t.Run(name, func(t *testing.T) {
			workflow := &store.TeamWorkflowData{OriginRouting: json.RawMessage(`{}`), CanonicalPlan: plan}
			if got := workflowLocale(workflow); got != "en" {
				t.Fatalf("workflowLocale = %q, want en", got)
			}
		})
	}
}

// End-to-end on the path that actually broke: the deterministic summary is the
// ONLY delivery on failing/cancelling (the LLM finalizer is skipped), so its
// header must match the language of the step titles it embeds.
func TestDeterministicWorkflowSummaryUsesPlanLanguageForFailure(t *testing.T) {
	t.Parallel()
	workflow := &store.TeamWorkflowData{
		Status:        store.TeamWorkflowStatusFailing,
		OriginRouting: json.RawMessage(`{}`),
		CanonicalPlan: viPlan(t),
	}
	tasks := []store.TeamTaskData{{
		WorkflowKind:   store.TeamWorkflowTaskKindWork,
		WorkflowStepID: "research-market-positioning",
		Subject:        "Phân tích thị trường và định vị",
		Status:         store.TeamTaskStatusFailed,
		Result:         strPtr("FAILED: iter 0 think: llm call: provider returned a successful response with no content, no tool calls and no usage (9router/Huy-Minh)"),
	}}

	summary := deterministicWorkflowSummary(workflow, tasks, workflowLocale(workflow))

	if strings.Contains(summary, "The request was partially processed") {
		t.Errorf("Vietnamese workflow delivered an English header:\n%s", summary)
	}
	if !strings.Contains(summary, "Yêu cầu đã được xử lý một phần") {
		t.Errorf("missing Vietnamese header:\n%s", summary)
	}
	if !strings.Contains(summary, "không hoàn thành do lỗi tạm thời") {
		t.Errorf("failure notice not in Vietnamese:\n%s", summary)
	}
	// The redaction guarantee must survive the locale change.
	for _, leak := range []string{"9router", "Huy-Minh", "iter 0 think", "llm call", "FAILED:"} {
		if strings.Contains(summary, leak) {
			t.Errorf("summary leaks %q:\n%s", leak, summary)
		}
	}
}

// looksVietnamese is a heuristic, so pin both directions explicitly. It must not
// flip on a stray diacritic inside otherwise-English text (a pasted proper noun),
// because that would deliver a Vietnamese notice to an English requester.
func TestLooksVietnamese(t *testing.T) {
	t.Parallel()
	vietnamese := []string{
		"Phân tích thị trường và định vị",
		"Xây dựng bộ tài liệu ra mắt dịch vụ",
		// Tone marks only, none of the seven unique letters.
		"quyết định này đã có các bên thống nhất",
	}
	for _, s := range vietnamese {
		if !looksVietnamese(s) {
			t.Errorf("looksVietnamese(%q) = false, want true", s)
		}
	}

	english := []string{
		"Research market and positioning",
		"Produce a consistent launch document set",
		"",
		// A single Vietnamese proper noun must not flip an English sentence.
		"Draft the onboarding email for Nguyễn and the rest of the team",
	}
	for _, s := range english {
		if looksVietnamese(s) {
			t.Errorf("looksVietnamese(%q) = true, want false", s)
		}
	}
}
