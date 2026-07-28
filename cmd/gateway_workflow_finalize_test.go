package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestWorkflowBackgroundContextOutlivesParentAndKeepsTenant(t *testing.T) {
	tenantID := uuid.New()
	parent, cancel := context.WithCancel(store.WithTenantID(context.Background(), tenantID))
	cancel()

	ctx := workflowBackgroundContext(parent)
	if err := ctx.Err(); err != nil {
		t.Fatalf("durable workflow context inherited cancellation: %v", err)
	}
	if got := store.TenantIDFromContext(ctx); got != tenantID {
		t.Fatalf("tenant = %s, want %s", got, tenantID)
	}
}

func TestWorkflowStepResultIsUsableRejectsNonDeliverables(t *testing.T) {
	// Observed live: khanh-developer's turn trailed off into "..." without calling
	// team_tasks(action="complete") and the step was settled COMPLETED with "..."
	// as its deliverable, so the critic reviewed "..." (workflow 019f9f21).
	unusable := []string{
		"",
		"   ",
		"...",
		". . .",
		"---",
		"**",
		"NO_REPLY",
		"no_reply",
		"_NO_REPLY_",
		"OK",
		"done",
		"Đã xong",
		"Step completed",
		"Agent run ended without explicit result",
	}
	for _, in := range unusable {
		if workflowStepResultIsUsable(in) {
			t.Errorf("expected %q to be rejected as a step deliverable", in)
		}
	}

	usable := []string{
		"Đã hoàn tất và lưu bản research cấu trúc tại goclaw-competitor-positioning-research.md.",
		"Architecture doc written to workspace: tenant isolation via schema-per-tenant, RLS fallback.",
		strings.Repeat("a", minUsableStepResultRunes),
	}
	for _, in := range usable {
		if !workflowStepResultIsUsable(in) {
			t.Errorf("expected %q to be accepted as a step deliverable", in)
		}
	}
}

func TestWorkflowStepResultIsUsableIgnoresGluedNoReplyWord(t *testing.T) {
	// IsSilentReply only matches a standalone token; a real result mentioning the
	// word must not be discarded.
	in := "The handler returns NO_REPLYING for muted channels, documented in handler.go."
	if !workflowStepResultIsUsable(in) {
		t.Fatalf("glued NO_REPLY variant must stay usable: %q", in)
	}
}
