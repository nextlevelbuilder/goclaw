package cmd

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildInboundPreRun is the seam that replaced the enqueue-time gate wiring with
// a dequeue-time hook (Phase 7 review 7A-H1). These tests exercise the returned
// closure directly — the same closure production hands the scheduler — so the
// gate → *agent.RunRequest + context wiring is verified in code, not asserted by
// a static-comment reference to gateway_consumer_normal.go line numbers.

// A fully validated orchestrating decision with a SUCCESSFUL audit must, at
// dequeue: copy the directive onto the request, leave team work enabled and no
// tools blocked, preserve the message, and thread BOTH the shared
// pending-dispatch tracker and the audit ID onto the run context.
func TestBuildInboundPreRun_OrchestratingDecisionWiresRequestAndContext(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingInboundTeamStore(nil, leadID, teamID)
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	provider := &inboundOrchestratingProvider{}
	agentLoop := deliveryRuntimeTestAgent{uuid: leadID, provider: provider, model: "test-model"}

	ptd := tools.NewPendingTeamDispatch()
	msg := bus.InboundMessage{Content: inboundIngressMsg, Metadata: map[string]string{}}
	preRun := buildInboundPreRun(deps, msg, "session:test", "team-lead", "direct", agentLoop, nil, "run:test", ptd)

	req := &agent.RunRequest{Message: inboundIngressMsg}
	runCtx, err := preRun(store.WithTenantID(context.Background(), tenantID), req)
	if err != nil {
		t.Fatalf("inbound hook must not abort the run, got err %v", err)
	}

	if !provider.called {
		t.Fatal("PreRun did not run the classifier")
	}
	if req.TeamWorkDirective == nil {
		t.Fatal("orchestrating decision must set the directive on the request at dequeue")
	}
	if req.DisableTeamWork {
		t.Fatal("orchestrating decision must not disable team work")
	}
	if len(req.BlockedTools) != 0 {
		t.Fatalf("orchestrating decision must not block tools, got %v", req.BlockedTools)
	}
	if req.Message != inboundIngressMsg {
		t.Fatalf("PreRun must preserve the run message, got %q", req.Message)
	}
	if got := tools.PendingTeamDispatchFromCtx(runCtx); got != ptd {
		t.Fatal("PreRun must thread the shared pending-dispatch tracker onto the run context")
	}
	if tools.TeamWorkClassificationAuditIDFromCtx(runCtx) == uuid.Nil {
		t.Fatal("successful audit must thread a non-nil audit ID onto the run context")
	}
	if teamStore.lastAudit == nil || teamStore.lastAudit.ID != tools.TeamWorkClassificationAuditIDFromCtx(runCtx) {
		t.Fatalf("context audit ID must match the persisted row: ctx=%s row=%+v",
			tools.TeamWorkClassificationAuditIDFromCtx(runCtx), teamStore.lastAudit)
	}
}

// A durable audit WRITE FAILURE on an orchestrating decision must fail closed at
// dequeue: no directive on the request, team work disabled, the three canonical
// orchestration tools blocked, and NO audit ID on the context (so a workflow
// cannot link back to a decision that was never durably recorded). The
// pending-dispatch tracker is still threaded — it is unconditional.
func TestBuildInboundPreRun_AuditFailureFailsClosedAtDequeue(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingInboundTeamStore(errors.New("audit db down"), leadID, teamID)
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	provider := &inboundOrchestratingProvider{}
	agentLoop := deliveryRuntimeTestAgent{uuid: leadID, provider: provider, model: "test-model"}

	ptd := tools.NewPendingTeamDispatch()
	msg := bus.InboundMessage{Content: inboundIngressMsg, Metadata: map[string]string{}}
	preRun := buildInboundPreRun(deps, msg, "session:test", "team-lead", "direct", agentLoop, nil, "run:test", ptd)

	req := &agent.RunRequest{Message: inboundIngressMsg}
	runCtx, err := preRun(store.WithTenantID(context.Background(), tenantID), req)
	if err != nil {
		t.Fatalf("inbound hook fails safe, it must not abort the run, got err %v", err)
	}

	if req.TeamWorkDirective != nil {
		t.Fatalf("audit failure must drop the directive, got %+v", req.TeamWorkDirective)
	}
	if !req.DisableTeamWork {
		t.Fatal("audit failure must disable team work on the request")
	}
	for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
		if !slices.Contains(req.BlockedTools, tool) {
			t.Fatalf("blocked tools = %v, missing %s", req.BlockedTools, tool)
		}
	}
	if tools.TeamWorkClassificationAuditIDFromCtx(runCtx) != uuid.Nil {
		t.Fatal("failed audit must not thread an audit ID onto the run context")
	}
	if got := tools.PendingTeamDispatchFromCtx(runCtx); got != ptd {
		t.Fatal("PreRun must thread the pending-dispatch tracker even on a fail-closed decision")
	}
}
