package cmd

import "testing"

// TestWorkflowStepResultIsUsableRejectsMemberRequestPlaceholders locks the
// predicate that now also guards member request task settlement (#146).
//
// Observed live (route-4 test, session test-route-member-1785164257): a member
// request task assigned to ly-content settled status=completed with result "..."
// (3 chars) — the finalizer's placeholder for an empty run — so the requester saw
// a done task with no work in it.
func TestWorkflowStepResultIsUsableRejectsMemberRequestPlaceholders(t *testing.T) {
	unusable := []string{
		"...",
		"…",
		"",
		"   ",
		"---",
		"**",
		"OK",
		"done",
		workflowStepPlaceholderCompleted,
		workflowStepPlaceholderNoResult,
	}
	for _, s := range unusable {
		if workflowStepResultIsUsable(s) {
			t.Errorf("result %q must not count as a deliverable", s)
		}
	}

	usable := []string{
		"Đã hoàn thành báo cáo phân tích thị trường nước hoa Việt Nam với 5 phát hiện chính.",
		"Here is the slugify function, tested against Vietnamese diacritics and edge cases.",
	}
	for _, s := range usable {
		if !workflowStepResultIsUsable(s) {
			t.Errorf("result %q is a real deliverable and must be accepted", s)
		}
	}
}
