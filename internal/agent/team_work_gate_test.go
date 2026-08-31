package agent

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
)

func gateTestInput() teamworkclassify.Input {
	return teamworkclassify.Input{
		Mode:                teamworkclassify.ModeTeam,
		TeamRole:            "lead",
		CanAssignTeamTasks:  true,
		CoordinatorAgentID:  uuid.New(),
		CoordinatorAgentKey: "lead",
	}
}

func assertGateFailedClosed(t *testing.T, decision TeamWorkGateDecision) {
	t.Helper()
	if decision.Directive != nil {
		t.Fatalf("fail-closed decision must have no directive: %+v", decision.Directive)
	}
	if !decision.DisableTeamWork {
		t.Fatal("fail-closed decision must disable team work for the run")
	}
	for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
		if !slices.Contains(decision.BlockedTools, tool) {
			t.Fatalf("fail-closed decision must block %q; blocked=%v", tool, decision.BlockedTools)
		}
	}
}

// A degraded/fail-safe classification must fail closed on both gate paths.
func TestBuildTeamWorkGateDecisionDegradedSelfFailsClosed(t *testing.T) {
	result := teamworkclassify.Result{
		Decision:           teamworkclassify.DecisionSelf,
		WorkflowMode:       teamworkclassify.WorkflowModeSelf,
		DegradedWorkflow:   true,
		DegradedReasonCode: "classifier_parse_failed",
	}
	decision := BuildTeamWorkGateDecision(result, gateTestInput(), "do the thing")
	assertGateFailedClosed(t, decision)
}

// An ACCEPTED self assessment (not degraded) must also fail closed: a turn the
// classifier resolved to self work still must not leave orchestration tools
// available. This is the accepted-self fail-open gap the shared helper closes.
func TestBuildTeamWorkGateDecisionAcceptedSelfFailsClosed(t *testing.T) {
	result := teamworkclassify.Result{
		Decision:         teamworkclassify.DecisionSelf,
		WorkflowMode:     teamworkclassify.WorkflowModeSelf,
		DegradedWorkflow: false,
		ValidatorReason:  "accepted self assessment",
	}
	decision := BuildTeamWorkGateDecision(result, gateTestInput(), "summarize this")
	assertGateFailedClosed(t, decision)
}

// A validated single-owner team decision keeps orchestration enabled with a
// directive and no plan constraint.
func TestBuildTeamWorkGateDecisionSingleOwnerTeamKeepsOrchestration(t *testing.T) {
	input := gateTestInput()
	ownerID := uuid.New()
	result := teamworkclassify.Result{
		Decision:        teamworkclassify.DecisionTeam,
		Mode:            teamworkclassify.ModeTeam,
		WorkflowMode:    teamworkclassify.WorkflowModeSingleOwner,
		RequiredTool:    "team_tasks",
		BestTeamOwner:   "runtime-owner",
		BestTeamOwnerID: ownerID,
		Reason:          "route to canonical owner",
	}
	decision := BuildTeamWorkGateDecision(result, input, "have the specialist do this")
	if decision.DisableTeamWork || len(decision.BlockedTools) != 0 {
		t.Fatalf("validated team decision must keep orchestration: %+v", decision)
	}
	if decision.Directive == nil {
		t.Fatalf("single-owner directive must exist: %+v", decision)
	}
	if decision.Directive.OriginalMessage != "have the specialist do this" || decision.Directive.BestTeamOwner != "runtime-owner" || decision.Directive.BestTeamOwnerID != ownerID {
		t.Fatalf("directive did not carry canonical routing context: %+v", decision.Directive)
	}
}

// An executable coordinated (multi_role) team route produces a coordinator
// directive: Mode is the coordinator discriminator, the canonical lead is carried
// as owner, orchestration stays enabled, and the required tool resolves to
// team_tasks so the lead can author the DAG.
func TestBuildTeamWorkGateDecisionCoordinatorDirective(t *testing.T) {
	input := gateTestInput()
	leadID := uuid.New()
	result := teamworkclassify.Result{
		Decision:              teamworkclassify.DecisionTeam,
		Mode:                  teamworkclassify.ModeTeam,
		WorkflowMode:          teamworkclassify.WorkflowModeMultiRole,
		RequestedWorkflowMode: teamworkclassify.WorkflowModeMultiRole,
		EffectiveWorkflowMode: teamworkclassify.WorkflowModeMultiRole,
		RequiredTool:          "team_tasks",
		WorkflowExecutable:    true,
		BestTeamOwner:         "team-lead",
		BestTeamOwnerID:       leadID,
		BestTeamOwnerRole:     "lead",
		Reason:                "parallel research and synthesis",
	}
	decision := BuildTeamWorkGateDecision(result, input, "draft, review, integrate")
	if decision.DisableTeamWork || len(decision.BlockedTools) != 0 {
		t.Fatalf("executable coordinated route must keep orchestration enabled: %+v", decision)
	}
	if decision.NonExecutable || decision.ConfigErrorCode != "" {
		t.Fatalf("executable coordinated route must not be marked non-executable: %+v", decision)
	}
	if decision.Directive == nil {
		t.Fatalf("coordinator directive must exist: %+v", decision)
	}
	if decision.Directive.Mode != TeamWorkDirectiveModeCoordinator {
		t.Fatalf("coordinator directive Mode = %q, want %q", decision.Directive.Mode, TeamWorkDirectiveModeCoordinator)
	}
	if decision.Directive.BestTeamOwner != "team-lead" || decision.Directive.BestTeamOwnerID != leadID || decision.Directive.BestTeamOwnerRole != "lead" {
		t.Fatalf("coordinator directive must carry the canonical lead as owner: %+v", decision.Directive)
	}
	if got := decision.Directive.normalizedRequiredTool(); got != "team_tasks" {
		t.Fatalf("coordinator directive required tool = %q, want team_tasks", got)
	}
}

// A coordinated route with EffectiveReviewRequired=true carries ReviewRequired on
// the coordinator directive so create_dag sees the enforcement flag on its ctx.
func TestBuildTeamWorkGateDecisionCoordinatorCarriesReviewRequired(t *testing.T) {
	input := gateTestInput()
	result := teamworkclassify.Result{
		Decision:                teamworkclassify.DecisionTeam,
		Mode:                    teamworkclassify.ModeTeam,
		WorkflowMode:            teamworkclassify.WorkflowModeMultiRole,
		RequestedWorkflowMode:   teamworkclassify.WorkflowModeMultiRole,
		EffectiveWorkflowMode:   teamworkclassify.WorkflowModeMultiRole,
		RequiredTool:            "team_tasks",
		WorkflowExecutable:      true,
		BestTeamOwner:           "team-lead",
		BestTeamOwnerID:         uuid.New(),
		BestTeamOwnerRole:       "lead",
		EffectiveReviewRequired: true,
	}
	decision := BuildTeamWorkGateDecision(result, input, "draft, review, integrate")
	if decision.Directive == nil {
		t.Fatal("coordinator directive must exist")
	}
	if decision.Directive.Mode != TeamWorkDirectiveModeCoordinator {
		t.Fatalf("directive Mode = %q, want coordinator", decision.Directive.Mode)
	}
	if !decision.Directive.ReviewRequired {
		t.Fatal("coordinator directive must carry ReviewRequired=true from EffectiveReviewRequired")
	}
}

// A non-executable coordinated team route fails closed with a stable
// configuration error code: no directive, team work disabled, orchestration tools
// blocked, and NonExecutable + ConfigErrorCode set from the degraded reason so the
// caller returns a user-facing configuration error instead of running the work.
func TestBuildTeamWorkGateDecisionNonExecutableFailsClosed(t *testing.T) {
	result := teamworkclassify.Result{
		Decision:              teamworkclassify.DecisionTeam,
		Mode:                  teamworkclassify.ModeTeam,
		WorkflowMode:          teamworkclassify.WorkflowModeMultiRole,
		RequestedWorkflowMode: teamworkclassify.WorkflowModeMultiRole,
		EffectiveWorkflowMode: teamworkclassify.WorkflowModeMultiRole,
		NonExecutable:         true,
		DegradedWorkflow:      true,
		DegradedReasonCode:    "canonical_coordinator_unavailable",
	}
	decision := BuildTeamWorkGateDecision(result, gateTestInput(), "draft, review, integrate")
	assertGateFailedClosed(t, decision)
	if !decision.NonExecutable {
		t.Fatal("non-executable coordinated route must set NonExecutable")
	}
	if decision.ConfigErrorCode != "canonical_coordinator_unavailable" {
		t.Fatalf("ConfigErrorCode = %q, want canonical_coordinator_unavailable", decision.ConfigErrorCode)
	}
}

// An executable multi_role route that somehow lacks a canonical owner cannot
// produce a coordinator directive; it fails closed to a plain self run (no
// NonExecutable marker, since this is not the classifier's non-executable path).
func TestBuildTeamWorkGateDecisionMultiRoleMissingOwnerFailsClosed(t *testing.T) {
	result := teamworkclassify.Result{
		Decision:           teamworkclassify.DecisionTeam,
		Mode:               teamworkclassify.ModeTeam,
		WorkflowMode:       teamworkclassify.WorkflowModeMultiRole,
		WorkflowExecutable: true,
	}
	decision := BuildTeamWorkGateDecision(result, gateTestInput(), "draft, review, integrate")
	assertGateFailedClosed(t, decision)
	if decision.NonExecutable || decision.ConfigErrorCode != "" {
		t.Fatalf("missing-owner multi_role route must fail closed without the non-executable marker: %+v", decision)
	}
}

// An accepted self assessment stays a plain fail-closed self run: no directive,
// no non-executable marker, no config error code.
func TestBuildTeamWorkGateDecisionSelfUnchanged(t *testing.T) {
	result := teamworkclassify.Result{
		Decision:     teamworkclassify.DecisionSelf,
		WorkflowMode: teamworkclassify.WorkflowModeSelf,
	}
	decision := BuildTeamWorkGateDecision(result, gateTestInput(), "summarize this")
	assertGateFailedClosed(t, decision)
	if decision.NonExecutable || decision.ConfigErrorCode != "" {
		t.Fatalf("self decision must not carry the non-executable marker: %+v", decision)
	}
}

// fakeAuditStore captures the audit write and can force a failure to exercise
// the audit-before-gate-returns fail-safe. successWithoutID returns a nil error
// but leaves audit.ID unset, simulating a store that reports success without a
// durable, linkable row.
type fakeAuditStore struct {
	err              error
	successWithoutID bool
	captured         *store.TeamWorkClassificationAudit
	calls            int
}

func (f *fakeAuditStore) RecordTeamWorkClassificationAudit(_ context.Context, audit *store.TeamWorkClassificationAudit) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	if f.successWithoutID {
		// Nil error, but ID deliberately left unset.
		f.captured = audit
		return nil
	}
	if audit.ID == uuid.Nil {
		audit.ID = uuid.New()
	}
	f.captured = audit
	return nil
}

func auditedSingleOwnerResult() teamworkclassify.Result {
	ownerID := uuid.New()
	return teamworkclassify.Result{
		Decision:              teamworkclassify.DecisionTeam,
		Mode:                  teamworkclassify.ModeTeam,
		WorkflowMode:          teamworkclassify.WorkflowModeSingleOwner,
		RequestedWorkflowMode: teamworkclassify.WorkflowModeSingleOwner,
		EffectiveWorkflowMode: teamworkclassify.WorkflowModeSingleOwner,
		RequiredTool:          "team_tasks",
		BestTeamOwner:         "runtime-owner",
		BestTeamOwnerID:       ownerID,
		Reason:                "route to canonical owner",
	}
}

// A successful audit write on an orchestrating decision keeps orchestration,
// returns the persisted audit ID, and records the frozen plan hash + request
// metadata (so a workflow created during the run can link back to it). The helper
// writes the audit BEFORE it returns the directive; the caller (WS/inbound
// dispatcher) is what then schedules the run, so this asserts audit-before-gate-
// returns, not audit-before-schedule (that ordering lives in the dispatchers and
// is covered by the ORDERING INVARIANT comments in chat.go / gateway_consumer_normal.go).
func TestBuildAuditedTeamWorkGateDecisionWritesAuditBeforeReturning(t *testing.T) {
	input := gateTestInput()
	result := auditedSingleOwnerResult()
	fake := &fakeAuditStore{}
	agentID := uuid.New()
	decision, auditID := BuildAuditedTeamWorkGateDecision(context.Background(), fake, result, input, teamworkclassify.ClassificationAuditInput{
		Ingress: store.TeamWorkIngressWS, RunID: "run-1", SessionKey: "sess-1", AgentID: &agentID,
		OriginalMessage: "draft, review, integrate", ClassifierProvider: "prov", ClassifierModel: "model",
	})
	if decision.Directive == nil {
		t.Fatalf("successful audit must keep the native directive: %+v", decision)
	}
	if auditID == uuid.Nil || fake.captured == nil || fake.captured.ID != auditID {
		t.Fatalf("audit ID must be returned and match the persisted row: id=%s captured=%+v", auditID, fake.captured)
	}
	if fake.calls != 1 {
		t.Fatalf("audit must be written exactly once, got %d", fake.calls)
	}
	if fake.captured.SelectedOwnerAgentID == nil || *fake.captured.SelectedOwnerAgentID != decision.Directive.BestTeamOwnerID || fake.captured.PlanHash != "" {
		t.Fatalf("audit must record the native selected owner without a plan hash: %+v", fake.captured)
	}
	if fake.captured.Ingress != store.TeamWorkIngressWS || fake.captured.RunID != "run-1" || fake.captured.SessionKey != "sess-1" {
		t.Fatalf("audit must record request metadata: %+v", fake.captured)
	}
	if fake.captured.EffectiveMode != store.TeamWorkModeSingleOwner || fake.captured.IndependentReview {
		t.Fatalf("audit must record the single-owner mode without independent review: %+v", fake.captured)
	}
}

// An audit write FAILURE on an orchestrating decision must fail closed to self:
// no orchestration is ever scheduled without a durable audit record.
func TestBuildAuditedTeamWorkGateDecisionWriteFailureFailsClosed(t *testing.T) {
	input := gateTestInput()
	result := auditedSingleOwnerResult()
	fake := &fakeAuditStore{err: errors.New("db down")}
	decision, auditID := BuildAuditedTeamWorkGateDecision(context.Background(), fake, result, input, teamworkclassify.ClassificationAuditInput{
		Ingress: store.TeamWorkIngressInbound, OriginalMessage: "draft, review, integrate",
	})
	assertGateFailedClosed(t, decision)
	if auditID != uuid.Nil {
		t.Fatalf("failed audit write must return no audit ID, got %s", auditID)
	}
}

// A nil audit store (misconfiguration) on an orchestrating decision must also
// fail closed rather than schedule orchestration with no audit trail.
func TestBuildAuditedTeamWorkGateDecisionNilStoreFailsClosed(t *testing.T) {
	input := gateTestInput()
	result := auditedSingleOwnerResult()
	decision, auditID := BuildAuditedTeamWorkGateDecision(context.Background(), nil, result, input, teamworkclassify.ClassificationAuditInput{
		Ingress: store.TeamWorkIngressWS, OriginalMessage: "draft, review, integrate",
	})
	assertGateFailedClosed(t, decision)
	if auditID != uuid.Nil {
		t.Fatalf("nil audit store must return no audit ID, got %s", auditID)
	}
}

// An audit write failure on a SELF decision costs only the audit row: the run
// was already self, so it stays self (no directive) without being forced through
// the fail-closed path a second time. No audit ID is returned.
func TestBuildAuditedTeamWorkGateDecisionSelfWriteFailureStaysSelf(t *testing.T) {
	result := teamworkclassify.Result{
		Decision: teamworkclassify.DecisionSelf, WorkflowMode: teamworkclassify.WorkflowModeSelf,
		DegradedWorkflow: true, DegradedReasonCode: "classifier_parse_failed",
		EffectiveWorkflowMode: teamworkclassify.WorkflowModeSelf,
	}
	fake := &fakeAuditStore{err: errors.New("db down")}
	decision, auditID := BuildAuditedTeamWorkGateDecision(context.Background(), fake, result, gateTestInput(), teamworkclassify.ClassificationAuditInput{
		Ingress: store.TeamWorkIngressWS, OriginalMessage: "summarize this",
	})
	assertGateFailedClosed(t, decision)
	if auditID != uuid.Nil {
		t.Fatalf("failed audit write must return no audit ID, got %s", auditID)
	}
}

// A nil error is NOT proof of a durable audit: if the store returns success but
// leaves audit.ID unset, an orchestrating decision must still fail closed — no
// orchestration may run linked to an unlinkable (uuid.Nil) audit row.
func TestBuildAuditedTeamWorkGateDecisionSuccessWithoutIDFailsClosed(t *testing.T) {
	input := gateTestInput()
	result := auditedSingleOwnerResult()
	fake := &fakeAuditStore{successWithoutID: true}
	decision, auditID := BuildAuditedTeamWorkGateDecision(context.Background(), fake, result, input, teamworkclassify.ClassificationAuditInput{
		Ingress: store.TeamWorkIngressWS, OriginalMessage: "draft, review, integrate",
	})
	if fake.calls != 1 {
		t.Fatalf("audit write must have been attempted once, got %d", fake.calls)
	}
	assertGateFailedClosed(t, decision)
	if auditID != uuid.Nil {
		t.Fatalf("success-without-ID must return no audit ID, got %s", auditID)
	}
}

// The same success-without-ID case on a SELF decision stays self (already had no
// directive) and returns no audit ID.
func TestBuildAuditedTeamWorkGateDecisionSelfSuccessWithoutIDStaysSelf(t *testing.T) {
	result := teamworkclassify.Result{
		Decision: teamworkclassify.DecisionSelf, WorkflowMode: teamworkclassify.WorkflowModeSelf,
		DegradedWorkflow: true, DegradedReasonCode: "classifier_parse_failed",
		EffectiveWorkflowMode: teamworkclassify.WorkflowModeSelf,
	}
	fake := &fakeAuditStore{successWithoutID: true}
	decision, auditID := BuildAuditedTeamWorkGateDecision(context.Background(), fake, result, gateTestInput(), teamworkclassify.ClassificationAuditInput{
		Ingress: store.TeamWorkIngressInbound, OriginalMessage: "summarize this",
	})
	assertGateFailedClosed(t, decision)
	if auditID != uuid.Nil {
		t.Fatalf("success-without-ID must return no audit ID, got %s", auditID)
	}
}

// The tenant's Team Work LLM budget must reach the agent loop's enforcement
// deadline, not just the classifier stages. Without this, raising the budget to
// accommodate a slow agent model still loses the run at the loop's built-in
// enforcement timeout — discarding a plan the classifier just validated.
func TestBuildTeamWorkGateDecisionCarriesEnforcementTimeout(t *testing.T) {
	input := gateTestInput()
	input.Timeout = 90 * time.Second
	result := teamworkclassify.Result{
		Decision:        teamworkclassify.DecisionTeam,
		Mode:            teamworkclassify.ModeTeam,
		WorkflowMode:    teamworkclassify.WorkflowModeSingleOwner,
		RequiredTool:    "team_tasks",
		BestTeamOwner:   "runtime-owner",
		BestTeamOwnerID: uuid.New(),
	}
	decision := BuildTeamWorkGateDecision(result, input, "have the specialist do this")
	if decision.Directive == nil {
		t.Fatal("validated team decision must produce a directive")
	}
	if got := decision.Directive.EnforcementTimeout; got != 90*time.Second {
		t.Fatalf("EnforcementTimeout = %v, want 90s", got)
	}
	if got := decision.Directive.enforcementAttemptTimeout(); got != 90*time.Second {
		t.Fatalf("enforcementAttemptTimeout() = %v, want the configured 90s", got)
	}
}

// An unset budget keeps the built-in enforcement deadline.
func TestEnforcementAttemptTimeoutFallsBackToDefault(t *testing.T) {
	if got := (*TeamWorkDirective)(nil).enforcementAttemptTimeout(); got != teamWorkEnforcementAttemptTimeout {
		t.Fatalf("nil directive timeout = %v, want %v", got, teamWorkEnforcementAttemptTimeout)
	}
	d := &TeamWorkDirective{}
	if got := d.enforcementAttemptTimeout(); got != teamWorkEnforcementAttemptTimeout {
		t.Fatalf("unset timeout = %v, want %v", got, teamWorkEnforcementAttemptTimeout)
	}
}

// Transient provider failures deserve another enforcement attempt; deterministic
// ones do not, because they fail identically the second time and only delay the
// user's answer.
func TestTeamWorkEnforcementRetryableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "http 503", err: &providers.HTTPError{Status: 503, Body: "service unavailable"}, want: true},
		{name: "http 429", err: &providers.HTTPError{Status: 429, Body: "rate limited"}, want: true},
		{name: "http 529 overloaded", err: &providers.HTTPError{Status: 529, Body: "overloaded"}, want: true},
		{name: "http2 header timeout", err: errors.New("http2: timeout awaiting response headers"), want: true},
		{name: "connection reset", err: errors.New("read tcp: connection reset by peer"), want: true},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "context overflow", err: &providers.HTTPError{Status: 400, Body: "maximum context length exceeded"}, want: false},
		{name: "auth", err: &providers.HTTPError{Status: 401, Body: "invalid api key"}, want: false},
		{name: "billing", err: &providers.HTTPError{Status: 402, Body: "credit balance too low"}, want: false},
		{name: "bad tool schema", err: &providers.HTTPError{Status: 400, Body: "invalid_request: tool_call malformed"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := teamWorkEnforcementRetryableError(tc.err); got != tc.want {
				t.Fatalf("teamWorkEnforcementRetryableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
