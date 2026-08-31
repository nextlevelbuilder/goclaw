package pg

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func pgRecoveryStrPtr(s string) *string { return &s }

// pgRecoveryFixture builds a tenant/lead/worker/team and returns the store + a
// tenant-scoped ctx so the recovery twins can drive the real attempt-fenced
// paths, mirroring sqliteRecoveryFixture for PG/SQLite parity (plan §4.1).
func pgRecoveryFixture(t *testing.T) (*PGTeamStore, context.Context, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := hooksTestDB(t)
	tenantID, leadID := seedTenantAndAgent(t, db)
	workerID, teamID := uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO agents (id,tenant_id,agent_key,agent_type,status,provider,model,owner_id)
		VALUES ($1,$2,$3,'predefined','active','test','test-model','owner')`, workerID, tenantID, "recovery-worker-"+workerID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES ($1,$2,$3,'active','{"version":2}','owner',$4)`, teamID, "Recovery Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	for id, role := range map[uuid.UUID]string{leadID: "lead", workerID: "member"} {
		if _, err := db.Exec(`INSERT INTO agent_team_members (team_id,agent_id,role,tenant_id) VALUES ($1,$2,$3,$4)`, teamID, id, role, tenantID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM team_task_comments WHERE tenant_id=$1", tenantID)
		db.Exec("DELETE FROM team_tasks WHERE tenant_id=$1", tenantID)
		db.Exec("DELETE FROM team_workflows WHERE tenant_id=$1", tenantID)
		db.Exec("DELETE FROM agent_team_members WHERE team_id=$1", teamID)
		db.Exec("DELETE FROM agent_teams WHERE id=$1", teamID)
		db.Exec("DELETE FROM agents WHERE id=$1", workerID)
	})
	return NewPGTeamStore(db), store.WithTenantID(context.Background(), tenantID), leadID, workerID, teamID
}

func TestPGGetTeamHydratesLeadIdentityForWorkflowReplan(t *testing.T) {
	s, ctx, leadID, _, teamID := pgRecoveryFixture(t)

	team, err := s.GetTeam(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if team == nil {
		t.Fatal("GetTeam returned nil")
	}
	if team.LeadAgentID != leadID {
		t.Fatalf("LeadAgentID = %s, want %s", team.LeadAgentID, leadID)
	}
	wantKey := "hook-agent-" + leadID.String()
	if team.LeadAgentKey != wantKey {
		t.Fatalf("LeadAgentKey = %q, want %q; workflow replan would falsely report coordinator changed", team.LeadAgentKey, wantKey)
	}
}

func pgMakeRunningWorkflow(t *testing.T, s *PGTeamStore, ctx context.Context, teamID, leadID uuid.UUID, tasks []store.TeamTaskData) *store.TeamWorkflowData {
	t.Helper()
	plan := []byte(`{"schema_version":1,"goal":"recovery","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "recovery-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
	}
	if err := s.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		t.Fatalf("CreateWorkflowWithTasks: %v", err)
	}
	return workflow
}

// pgDriveToBlocked runs the real fenced path claim→accept→block and returns the
// stale attempt (whose token no longer owns the task after the block clears it).
func pgDriveToBlocked(t *testing.T, s *PGTeamStore, ctx context.Context, workflowID, taskID, teamID uuid.UUID, reason string) store.WorkflowTaskAttempt {
	t.Helper()
	token, err := s.ClaimWorkflowTaskDispatch(ctx, taskID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim dispatch: %v", err)
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	attempt := store.WorkflowTaskAttempt{TeamID: teamID, WorkflowID: workflowID, TaskID: taskID, DispatchToken: token, PlanRevision: task.PlanRevision, WorkflowStep: task.WorkflowStepID}
	if tr, err := s.AcceptWorkflowTaskAttempt(ctx, attempt, time.Now().Add(time.Hour)); err != nil || !tr.Applied() {
		t.Fatalf("accept attempt outcome=%v err=%v", tr.Outcome, err)
	}
	if tr, err := s.BlockWorkflowTaskAttempt(ctx, attempt, reason); err != nil || !tr.Applied() {
		t.Fatalf("block attempt outcome=%v err=%v", tr.Outcome, err)
	}
	return attempt
}

// TestPGWorkflowPersistsClassificationAuditLink proves the classification audit
// ID set on a workflow is written by the INSERT and read back by the SELECT —
// the last hop of gate → run context → workflow builder → DB. A prior INSERT
// omission left this column NULL despite the builder stamping it, so this
// asserts the persisted round-trip, not just the in-memory field.
func TestPGWorkflowPersistsClassificationAuditLink(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)

	// The workflow FK requires a real audit row first.
	audit := &store.TeamWorkClassificationAudit{Ingress: store.TeamWorkIngressWS, AgentID: &leadID}
	if err := teamStore.RecordTeamWorkClassificationAudit(ctx, audit); err != nil {
		t.Fatalf("record audit: %v", err)
	}
	if audit.ID == uuid.Nil {
		t.Fatal("audit ID must be populated")
	}

	rootID := uuid.New()
	plan := []byte(`{"schema_version":1,"goal":"audited","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "audited-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
		ClassificationAuditID: &audit.ID,
	}
	if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	}); err != nil {
		t.Fatalf("CreateWorkflowWithTasks: %v", err)
	}

	got, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.ClassificationAuditID == nil {
		t.Fatal("classification_audit_id must survive the INSERT and be read back (was NULL)")
	}
	if *got.ClassificationAuditID != audit.ID {
		t.Fatalf("audit link = %s, want %s", *got.ClassificationAuditID, audit.ID)
	}
}

// pgDriveToCompleted runs the real fenced path claim→accept→complete and
// returns the attempt whose token the completion UPDATE cleared to NULL. A
// replay of CompleteWorkflowTaskAttempt with this attempt must classify as
// AlreadyApplied (idempotent), NOT Stale — the G4 defect: settle returned
// Stale on the cleared-token replay, so dependents never dispatched.
func pgDriveToCompleted(t *testing.T, s *PGTeamStore, ctx context.Context, workflowID, taskID, teamID uuid.UUID, result string) store.WorkflowTaskAttempt {
	t.Helper()
	token, err := s.ClaimWorkflowTaskDispatch(ctx, taskID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim dispatch: %v", err)
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	attempt := store.WorkflowTaskAttempt{TeamID: teamID, WorkflowID: workflowID, TaskID: taskID, DispatchToken: token, PlanRevision: task.PlanRevision, WorkflowStep: task.WorkflowStepID}
	if tr, err := s.AcceptWorkflowTaskAttempt(ctx, attempt, time.Now().Add(time.Hour)); err != nil || !tr.Applied() {
		t.Fatalf("accept attempt outcome=%v err=%v", tr.Outcome, err)
	}
	if tr, err := s.CompleteWorkflowTaskAttempt(ctx, attempt, result); err != nil || !tr.Applied() {
		t.Fatalf("complete attempt outcome=%v err=%v", tr.Outcome, err)
	}
	return attempt
}

// TestPGCompleteWorkflowTaskAttemptReplayAfterCompletionIsAlreadyApplied proves
// the G4 idempotent-replay fix: after the tool path completes a workflow step,
// the same attempt's token is cleared to NULL. The post-turn settle replays
// CompleteWorkflowTaskAttempt with that now-stale-looking token. Before the fix
// the probe returned Stale (token nil → not equal), settle returned early, and
// every dependent stayed blocked behind the completed root. After the fix the
// probe recognizes the cleared-token + matching-revision + target-status
// fingerprint as the same attempt already applied, settles AlreadyApplied, and
// the consumer falls through to DispatchUnblockedTasks.
func TestPGCompleteWorkflowTaskAttemptReplayAfterCompletionIsAlreadyApplied(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID, dependentID := uuid.New(), uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: dependentID}, TeamID: teamID, Subject: "Dependent", Status: store.TeamTaskStatusBlocked, OwnerAgentID: &leadID, WorkflowStepID: "final", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, BlockedBy: []uuid.UUID{rootID}},
	})

	attempt := pgDriveToCompleted(t, teamStore, ctx, workflow.ID, rootID, teamID, "root done")

	// Completion cleared the dispatch token and unblocked the dependent.
	completed, err := teamStore.GetTask(ctx, rootID)
	if err != nil || completed.Status != store.TeamTaskStatusCompleted || completed.DispatchToken != nil {
		t.Fatalf("completed root=%+v err=%v", completed, err)
	}
	dependent, err := teamStore.GetTask(ctx, dependentID)
	if err != nil || dependent.Status != store.TeamTaskStatusPending {
		t.Fatalf("dependent must unblock to pending after root completes, got=%+v err=%v", dependent, err)
	}

	// The replay: settle calls CompleteWorkflowTaskAttempt again with the same
	// (now NULL-token) attempt. This MUST be AlreadyApplied, not Stale.
	replay, err := teamStore.CompleteWorkflowTaskAttempt(ctx, attempt, "root done")
	if err != nil {
		t.Fatalf("replay complete err=%v", err)
	}
	if replay.Outcome != store.WorkflowMutationAlreadyApplied {
		t.Fatalf("replay of cleared-token attempt outcome=%v, want AlreadyApplied (Stale strands dependents)", replay.Outcome)
	}
	if replay.WorkflowStatus != store.TeamWorkflowStatusRunning {
		t.Fatalf("replay workflow status=%v, want running (settle must fall through to DispatchUnblockedTasks)", replay.WorkflowStatus)
	}
}

// TestPGCompleteWorkflowTaskAttemptReplayAfterNewerAttemptIsStale proves the fix
// preserves the supersession invariant: when a newer attempt owns the task (here
// the recovery path re-queued it to pending and a fresh claim minted a new
// token), a replay of the OLD attempt must still return Stale. The cleared-token
// exception only applies when the revision matches AND the task sits in a target
// state the same attempt drove it to — a newer token breaks that fingerprint.
func TestPGCompleteWorkflowTaskAttemptReplayAfterNewerAttemptIsStale(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	// Drive to blocked, then the coordinator retries — which clears the old
	// attempt's token and moves the task back to pending, same revision.
	oldAttempt := pgDriveToBlocked(t, teamStore, ctx, workflow.ID, rootID, teamID, "needs API key")
	if tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, "use the staging key"); err != nil || !tr.Applied() {
		t.Fatalf("retry transition=%+v err=%v", tr, err)
	}
	// A fresh claim mints a NEWER token for the same task/revision.
	newToken, err := teamStore.ClaimWorkflowTaskDispatch(ctx, rootID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("fresh claim dispatch: %v", err)
	}

	// The old attempt's replay must be Stale: the task now holds a different,
	// non-nil token. This is the real-supersession path the fix must NOT relax.
	task, err := teamStore.GetTask(ctx, rootID)
	if err != nil || task.DispatchToken == nil || *task.DispatchToken != newToken {
		t.Fatalf("task must hold the newer token, got=%+v err=%v", task, err)
	}
	if task.PlanRevision != oldAttempt.PlanRevision {
		t.Fatalf("revision moved unexpectedly: %d vs %d", task.PlanRevision, oldAttempt.PlanRevision)
	}
	replay, err := teamStore.CompleteWorkflowTaskAttempt(ctx, oldAttempt, "late result")
	if err != nil {
		t.Fatalf("replay complete err=%v", err)
	}
	if replay.Outcome != store.WorkflowMutationStale {
		t.Fatalf("replay against a newer-token owner outcome=%v, want Stale", replay.Outcome)
	}
	// The task must be untouched by the stale replay.
	if still, err := teamStore.GetTask(ctx, rootID); err != nil || still.Status != store.TeamTaskStatusDispatching || still.DispatchToken == nil || *still.DispatchToken != newToken {
		t.Fatalf("stale replay must leave the newer attempt intact, got=%+v err=%v", still, err)
	}
}

func TestPGRetryBlockedWorkflowTaskResetsOnlyAffectedTask(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID, terminalID := uuid.New(), uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	staleAttempt := pgDriveToBlocked(t, teamStore, ctx, workflow.ID, rootID, teamID, "needs API key")

	blocked, err := teamStore.GetTask(ctx, rootID)
	if err != nil || blocked.Status != store.TeamTaskStatusBlocked || blocked.EscalationStatus != store.TeamTaskEscalationPending || blocked.BlockerReason != "needs API key" {
		t.Fatalf("blocked task=%+v err=%v", blocked, err)
	}
	if wf, err := teamStore.GetWorkflow(ctx, workflow.ID); err != nil || wf.Status != store.TeamWorkflowStatusRunning {
		t.Fatalf("workflow status=%v err=%v, want running", wf.Status, err)
	}
	if other, err := teamStore.GetTask(ctx, terminalID); err != nil || other.Status != store.TeamTaskStatusPending {
		t.Fatalf("independent terminal task=%v err=%v, want untouched pending", other.Status, err)
	}

	tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, "use the staging key")
	if err != nil || !tr.Applied() || tr.TaskStatus != store.TeamTaskStatusPending || tr.WorkflowID != workflow.ID {
		t.Fatalf("retry transition=%+v err=%v", tr, err)
	}
	retried, err := teamStore.GetTask(ctx, rootID)
	if err != nil || retried.Status != store.TeamTaskStatusPending || retried.RecoveryCount != 1 || retried.BlockerReason != "" || retried.EscalationStatus != store.TeamTaskEscalationDelivered {
		t.Fatalf("retried task=%+v err=%v", retried, err)
	}
	if retried.OwnerAgentID == nil || *retried.OwnerAgentID != workerID {
		t.Fatalf("retry must preserve owner, got %v", retried.OwnerAgentID)
	}

	if hb, err := teamStore.HeartbeatWorkflowTaskAttempt(ctx, staleAttempt, time.Now().Add(time.Hour)); err != nil || !hb.Stale() {
		t.Fatalf("stale heartbeat outcome=%v err=%v", hb.Outcome, err)
	}
	if cp, err := teamStore.CompleteWorkflowTaskAttempt(ctx, staleAttempt, "late result"); err != nil || !cp.Stale() {
		t.Fatalf("stale complete outcome=%v err=%v", cp.Outcome, err)
	}
	if still, err := teamStore.GetTask(ctx, rootID); err != nil || still.Status != store.TeamTaskStatusPending {
		t.Fatalf("task must stay pending after stale attempts, got %v err=%v", still.Status, err)
	}

	if tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, ""); err != nil || tr.Outcome != store.WorkflowMutationStale {
		t.Fatalf("retry of non-blocked task outcome=%v err=%v", tr.Outcome, err)
	}
}

func TestPGCancelWorkflowCancelsNonTerminalTasks(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	doneID, activeID := uuid.New(), uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Completed", Status: store.TeamTaskStatusCompleted, Result: pgRecoveryStrPtr("kept"), OwnerAgentID: &workerID, WorkflowStepID: "one", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: activeID}, TeamID: teamID, Subject: "Active", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "two", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	cancelled, err := teamStore.CancelWorkflow(ctx, workflow.ID, teamID, "user aborted")
	if err != nil || cancelled.Status != store.TeamWorkflowStatusCancelling || cancelled.CancelReason != "user aborted" {
		t.Fatalf("cancel workflow=%+v err=%v", cancelled, err)
	}
	if done, err := teamStore.GetTask(ctx, doneID); err != nil || done.Status != store.TeamTaskStatusCompleted || done.Result == nil || *done.Result != "kept" {
		t.Fatalf("completed result must survive cancel: %+v err=%v", done, err)
	}
	if active, err := teamStore.GetTask(ctx, activeID); err != nil || active.Status != store.TeamTaskStatusCancelled {
		t.Fatalf("active task=%v err=%v, want cancelled", active.Status, err)
	}
	if _, err := teamStore.CancelWorkflow(ctx, workflow.ID, teamID, "again"); err == nil {
		t.Fatal("cancelling a non-cancellable workflow must fail")
	}
}

// TestPGCancelWorkflowFinalizesToCancelled proves the cancelling→cancelled
// transition is not a dead-end: after CancelWorkflow moves the workflow to
// cancelling (and cancels its non-terminal work tasks in the same transaction),
// the finalizer can discover it via ListWorkflowsReadyToFinalize, claim it, and
// commit the terminal cancelled status with a durable summary. PG parity of
// TestSQLiteCancelWorkflowFinalizesToCancelled.
func TestPGCancelWorkflowFinalizesToCancelled(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	doneID, activeID := uuid.New(), uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Completed", Status: store.TeamTaskStatusCompleted, Result: pgRecoveryStrPtr("kept"), OwnerAgentID: &workerID, WorkflowStepID: "one", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: activeID}, TeamID: teamID, Subject: "Active", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "two", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	if _, err := teamStore.CancelWorkflow(ctx, workflow.ID, teamID, "user aborted"); err != nil {
		t.Fatalf("cancel workflow: %v", err)
	}

	// The finalizer ticker scans cross-tenant. A cancelling workflow with no active
	// dispatching/in_progress work tasks is ready to finalize (CancelWorkflow already
	// cancelled the only non-terminal task in-transaction).
	crossCtx := store.WithCrossTenant(ctx)
	ready, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, time.Now())
	if err != nil {
		t.Fatalf("list ready: %v", err)
	}
	found := false
	for _, scope := range ready {
		if scope.WorkflowID == workflow.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cancelling workflow must be ready to finalize")
	}

	fin, token, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
	if err != nil || fin == nil || fin.Status != store.TeamWorkflowStatusCancelling {
		t.Fatalf("claim finalization=%+v err=%v", fin, err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, token, store.TeamWorkflowStatusCancelled, "Cancelled: user aborted"); err != nil {
		t.Fatalf("complete finalization: %v", err)
	}
	final, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil || final.Status != store.TeamWorkflowStatusCancelled || final.ResultSummary != "Cancelled: user aborted" || final.FinalizedAt == nil {
		t.Fatalf("finalized workflow=%+v err=%v", final, err)
	}
	// Completed evidence stays immutable through the whole cancel→finalize path.
	if done, err := teamStore.GetTask(ctx, doneID); err != nil || done.Status != store.TeamTaskStatusCompleted || done.Result == nil || *done.Result != "kept" {
		t.Fatalf("completed result must survive cancel/finalize: %+v err=%v", done, err)
	}
}

func TestPGFailWorkflowExpansionBoundedBackoffThenFailing(t *testing.T) {
	teamStore, ctx, leadID, _, teamID := pgRecoveryFixture(t)
	plan := []byte(`{"schema_version":1,"goal":"expand","steps":[]}`)
	pending := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusPendingExpansion, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "expand-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
		AutoExpand: true, // the ticker only auto-expands workflows flagged for it
	}
	audit := &store.TeamTaskData{TeamID: teamID, Subject: "Request", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, TaskType: "request", CreatedByAgentID: &leadID}
	if err := teamStore.CreatePendingWorkflowRequest(ctx, pending, audit); err != nil {
		t.Fatalf("CreatePendingWorkflowRequest: %v", err)
	}

	token, err := teamStore.ClaimPendingWorkflowExpansion(ctx, pending.ID, leadID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim expansion: %v", err)
	}
	w, err := teamStore.FailWorkflowExpansion(ctx, pending.ID, leadID, token, "provider timeout", true)
	if err != nil || w.Status != store.TeamWorkflowStatusPendingExpansion || w.ExpansionAttemptCount != 1 || w.NextExpansionAt == nil || w.LastExpansionError != "provider timeout" {
		t.Fatalf("transient expansion fail=%+v err=%v", w, err)
	}
	if _, err := teamStore.FailWorkflowExpansion(ctx, pending.ID, leadID, token, "stale", true); err == nil {
		t.Fatal("stale expansion token must be rejected")
	}
	// The ticker query honors the backoff: the workflow is hidden until
	// next_expansion_at, then reappears. This is what stops the pre-fix tight retry.
	crossCtx := store.WithCrossTenant(ctx)
	if due, err := teamStore.ListPendingAutoExpandWorkflows(crossCtx, w.NextExpansionAt.Add(-time.Second)); err != nil || pgContainsWorkflow(due, pending.ID) {
		t.Fatalf("expansion must be hidden before next_expansion_at: due=%d err=%v", len(due), err)
	}
	if due, err := teamStore.ListPendingAutoExpandWorkflows(crossCtx, w.NextExpansionAt.Add(time.Second)); err != nil || !pgContainsWorkflow(due, pending.ID) {
		t.Fatalf("expansion must be due after next_expansion_at: due=%d err=%v", len(due), err)
	}

	token, err = teamStore.ClaimPendingWorkflowExpansion(ctx, pending.ID, leadID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("re-claim expansion: %v", err)
	}
	w, err = teamStore.FailWorkflowExpansion(ctx, pending.ID, leadID, token, "roster invalidated", false)
	if err != nil || w.Status != store.TeamWorkflowStatusFailing || w.FailureSummary == "" {
		t.Fatalf("non-transient expansion fail=%+v err=%v", w, err)
	}
}

func TestPGFailWorkflowDeliveryAttemptBoundedThenDead(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Only", Status: store.TeamTaskStatusCompleted, Result: pgRecoveryStrPtr("done"), OwnerAgentID: &workerID, WorkflowStepID: "only", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})
	fin, finToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
	if err != nil || fin == nil {
		t.Fatalf("claim finalization: %v", err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, finToken, store.TeamWorkflowStatusCompleted, "final result"); err != nil {
		t.Fatalf("complete finalization: %v", err)
	}

	var lastStatus string
	for i := 1; i <= store.MaxWorkflowDeliveryAttempts; i++ {
		_, token, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("claim delivery attempt %d: %v", i, err)
		}
		w, err := teamStore.FailWorkflowDeliveryAttempt(ctx, workflow.ID, token, fmt.Sprintf("channel down %d", i))
		if err != nil {
			t.Fatalf("fail delivery attempt %d: %v", i, err)
		}
		if w.DeliveryAttemptCount != i {
			t.Fatalf("attempt %d: delivery_attempt_count=%d", i, w.DeliveryAttemptCount)
		}
		lastStatus = w.DeliveryStatus
		if i < store.MaxWorkflowDeliveryAttempts {
			if w.DeliveryStatus != store.TeamWorkflowDeliveryPending || w.NextDeliveryAt == nil {
				t.Fatalf("attempt %d must schedule retry: status=%q next=%v", i, w.DeliveryStatus, w.NextDeliveryAt)
			}
			// The finalize re-drive query honors the delivery backoff: hidden before
			// next_delivery_at, due after. This is what stops the pre-fix tight re-claim.
			crossCtx := store.WithCrossTenant(ctx)
			if due, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, w.NextDeliveryAt.Add(-time.Second)); err != nil || pgContainsScope(due, workflow.ID) {
				t.Fatalf("attempt %d: delivery must be hidden before next_delivery_at: ready=%d err=%v", i, len(due), err)
			}
			if due, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, w.NextDeliveryAt.Add(time.Second)); err != nil || !pgContainsScope(due, workflow.ID) {
				t.Fatalf("attempt %d: delivery must be due after next_delivery_at: ready=%d err=%v", i, len(due), err)
			}
			if _, err := teamStore.db.Exec(`UPDATE team_workflows SET next_delivery_at=$1 WHERE id=$2`, time.Now().Add(-time.Second), workflow.ID); err != nil {
				t.Fatalf("make delivery attempt %d due: %v", i+1, err)
			}
		}
	}
	if lastStatus != store.TeamWorkflowDeliveryDead {
		t.Fatalf("delivery must be dead after %d attempts, got %q", store.MaxWorkflowDeliveryAttempts, lastStatus)
	}
	if w, err := teamStore.GetWorkflow(ctx, workflow.ID); err != nil || w.ResultSummary != "final result" || w.LastDeliveryError == "" {
		t.Fatalf("dead-delivery workflow=%+v err=%v", w, err)
	}
	// A dead delivery is permanently excluded from the finalize re-drive so the
	// operator-visible dead state is terminal, not re-claimed forever.
	crossCtx := store.WithCrossTenant(ctx)
	if due, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, time.Now().Add(time.Hour)); err != nil || pgContainsScope(due, workflow.ID) {
		t.Fatalf("dead delivery must never be re-claimed: ready=%d err=%v", len(due), err)
	}
}

// TestPGWorkflowEscalationRetryPersistsUntilResolution is the PG twin of
// TestSQLiteWorkflowEscalationRetryPersistsUntilResolution: it proves the durable
// coordinator-escalation ledger survives an enqueue/recovery-run failure. Each
// ClaimTaskEscalation reschedules the next capped-backoff retry, so a dropped
// hand-off leaves the escalation armed and re-claimable rather than silently
// lost. Only a real coordinator resolution (retry/replan/cancel) clears it.
func TestPGWorkflowEscalationRetryPersistsUntilResolution(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})
	pgDriveToBlocked(t, teamStore, ctx, workflow.ID, rootID, teamID, "needs API key")

	// Blocking arms a durable escalation-pending state with a due next_at.
	blocked, err := teamStore.GetTask(ctx, rootID)
	if err != nil || blocked.EscalationStatus != store.TeamTaskEscalationPending || blocked.EscalationNextAt == nil {
		t.Fatalf("blocked escalation state=%+v err=%v", blocked, err)
	}

	crossCtx := store.WithCrossTenant(ctx)
	// The recovery ticker sweep surfaces the due escalation.
	due, err := teamStore.ListEscalationDueTasks(crossCtx, blocked.EscalationNextAt.Add(time.Second))
	if err != nil || !pgContainsTask(due, rootID) {
		t.Fatalf("escalation must be due for the sweep: due=%d err=%v", len(due), err)
	}

	// First claim: bump attempt, move to enqueuing, schedule the next re-claim.
	claim, err := teamStore.ClaimTaskEscalation(ctx, rootID, teamID, blocked.EscalationNextAt.Add(time.Second))
	if err != nil || !claim.Claimed || claim.Attempt != 1 {
		t.Fatalf("first escalation claim=%+v err=%v", claim, err)
	}
	afterClaim, err := teamStore.GetTask(ctx, rootID)
	if err != nil || afterClaim.EscalationStatus != store.TeamTaskEscalationEnqueuing || afterClaim.EscalationAttemptCount != 1 || afterClaim.EscalationNextAt == nil {
		t.Fatalf("after first claim=%+v err=%v", afterClaim, err)
	}

	// Simulate an enqueue / recovery-run failure: the caller does NOT resolve the
	// blocker. The durable escalation stays armed and re-appears once the newly
	// scheduled backoff elapses — the retry is not lost.
	if stillDue, err := teamStore.ListEscalationDueTasks(crossCtx, afterClaim.EscalationNextAt.Add(-time.Second)); err != nil || pgContainsTask(stillDue, rootID) {
		t.Fatalf("escalation must be hidden before its rescheduled next_at: due=%d err=%v", len(stillDue), err)
	}
	reDue, err := teamStore.ListEscalationDueTasks(crossCtx, afterClaim.EscalationNextAt.Add(time.Second))
	if err != nil || !pgContainsTask(reDue, rootID) {
		t.Fatalf("escalation must be re-claimable after its rescheduled next_at: due=%d err=%v", len(reDue), err)
	}
	claim2, err := teamStore.ClaimTaskEscalation(ctx, rootID, teamID, afterClaim.EscalationNextAt.Add(time.Second))
	if err != nil || !claim2.Claimed || claim2.Attempt != 2 {
		t.Fatalf("second escalation claim=%+v err=%v", claim2, err)
	}

	// A real coordinator resolution clears the escalation ledger for good.
	if tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, "use the staging key"); err != nil || !tr.Applied() {
		t.Fatalf("retry transition=%+v err=%v", tr, err)
	}
	resolved, err := teamStore.GetTask(ctx, rootID)
	if err != nil || resolved.EscalationStatus != store.TeamTaskEscalationDelivered || resolved.Status != store.TeamTaskStatusPending {
		t.Fatalf("resolved escalation state=%+v err=%v", resolved, err)
	}
	// After resolution the escalation is no longer due to any sweep.
	if gone, err := teamStore.ListEscalationDueTasks(crossCtx, time.Now().Add(time.Hour)); err != nil || pgContainsTask(gone, rootID) {
		t.Fatalf("resolved escalation must never be due again: due=%d err=%v", len(gone), err)
	}
}

func pgContainsTask(tasks []store.TeamTaskData, id uuid.UUID) bool {
	for _, task := range tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

func pgContainsWorkflow(workflows []store.TeamWorkflowData, id uuid.UUID) bool {
	for _, w := range workflows {
		if w.ID == id {
			return true
		}
	}
	return false
}

func pgContainsScope(scopes []store.TeamWorkflowDispatchScope, id uuid.UUID) bool {
	for _, s := range scopes {
		if s.WorkflowID == id {
			return true
		}
	}
	return false
}

// TestPGCompleteWorkflowTaskAttemptReplayAlreadyAppliedReadyToFinalizeTrue
// proves the ReadyToFinalize fix: when the tool path completed the terminal
// step during the turn (clearing the token), the post-turn settle replays
// CompleteWorkflowTaskAttempt and the probe classifies it AlreadyApplied.
// Before the fix the replay's ReadyToFinalize stayed the zero value (only the
// 1-row branch computed it), so settle returned before finalizeWorkflow and
// the workflow stayed running with all tasks completed. The replay must carry
// ReadyToFinalize=true so settle reaches finalizeWorkflow.
func TestPGCompleteWorkflowTaskAttemptReplayAlreadyAppliedReadyToFinalizeTrue(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID, terminalID := uuid.New(), uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Terminal", Status: store.TeamTaskStatusBlocked, OwnerAgentID: &leadID, WorkflowStepID: "final", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, BlockedBy: []uuid.UUID{rootID}},
	})

	// Drive both steps to completed via the tool path. Root completes first,
	// unblocking the terminal; terminal then completes — leaving zero
	// non-terminal work tasks in this revision.
	rootAttempt := pgDriveToCompleted(t, teamStore, ctx, workflow.ID, rootID, teamID, "root done")
	// Root completion unblocks the terminal (blocked→pending) — retry is not
	// needed and would return Stale because the task is no longer blocked.
	terminalAttempt := pgDriveToCompleted(t, teamStore, ctx, workflow.ID, terminalID, teamID, "terminal done")

	// Sanity: the terminal's own completion (1-row branch) reported ReadyToFinalize.
	terminalComplete, err := teamStore.CompleteWorkflowTaskAttempt(ctx, terminalAttempt, "terminal done")
	if err != nil || terminalComplete.Outcome != store.WorkflowMutationAlreadyApplied {
		t.Fatalf("terminal replay outcome=%v err=%v (want AlreadyApplied)", terminalComplete.Outcome, err)
	}
	if !terminalComplete.ReadyToFinalize {
		t.Fatalf("terminal replay ReadyToFinalize=false, want true — finalizeWorkflow would never run and the workflow would strand running")
	}

	// The root replay must also report ReadyToFinalize=true now that the
	// terminal has completed: settle honors it regardless of which step's
	// replay carried the signal.
	rootReplay, err := teamStore.CompleteWorkflowTaskAttempt(ctx, rootAttempt, "root done")
	if err != nil {
		t.Fatalf("root replay err=%v", err)
	}
	if rootReplay.Outcome != store.WorkflowMutationAlreadyApplied {
		t.Fatalf("root replay outcome=%v, want AlreadyApplied", rootReplay.Outcome)
	}
	if !rootReplay.ReadyToFinalize {
		t.Fatalf("root replay ReadyToFinalize=false, want true — workflow has no non-terminal work task left in this revision")
	}
}

// TestPGCompleteWorkflowTaskAttemptReplayAlreadyAppliedReadyToFinalizeFalse
// proves the fix does not over-fire: when the replayed step was completed but
// another work task in the same revision is still non-terminal, the replay
// must carry ReadyToFinalize=false so finalizeWorkflow is NOT armed (the
// workflow has remaining work). This preserves the invariant that finalize
// only converges once every work task in the revision is terminal.
func TestPGCompleteWorkflowTaskAttemptReplayAlreadyAppliedReadyToFinalizeFalse(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID, siblingID := uuid.New(), uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork},
		// Sibling is an independent, still-pending work task — the workflow is NOT ready.
		{BaseModel: store.BaseModel{ID: siblingID}, TeamID: teamID, Subject: "Sibling", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "sibling", WorkflowKind: store.TeamWorkflowTaskKindWork},
	})

	rootAttempt := pgDriveToCompleted(t, teamStore, ctx, workflow.ID, rootID, teamID, "root done")

	// Sibling is still pending — the workflow has remaining non-terminal work.
	sibling, err := teamStore.GetTask(ctx, siblingID)
	if err != nil || sibling.Status != store.TeamTaskStatusPending {
		t.Fatalf("sibling must remain pending, got=%+v err=%v", sibling, err)
	}

	replay, err := teamStore.CompleteWorkflowTaskAttempt(ctx, rootAttempt, "root done")
	if err != nil {
		t.Fatalf("root replay err=%v", err)
	}
	if replay.Outcome != store.WorkflowMutationAlreadyApplied {
		t.Fatalf("root replay outcome=%v, want AlreadyApplied", replay.Outcome)
	}
	if replay.ReadyToFinalize {
		t.Fatalf("root replay ReadyToFinalize=true, want false — a non-terminal work task (sibling) still exists in this revision; finalize must not fire")
	}
}

// TestPGCompleteWorkflowTaskAttemptReplayStalePreservesNoReadyToFinalize
// confirms the supersession path remains untouched: a Stale replay must not
// compute ReadyToFinalize (it must stay false), so finalize is never armed by
// a superseded attempt.
func TestPGCompleteWorkflowTaskAttemptReplayStalePreservesNoReadyToFinalize(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	rootID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	oldAttempt := pgDriveToBlocked(t, teamStore, ctx, workflow.ID, rootID, teamID, "needs API key")
	if tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, "use the staging key"); err != nil || !tr.Applied() {
		t.Fatalf("retry transition=%+v err=%v", tr, err)
	}
	newToken, err := teamStore.ClaimWorkflowTaskDispatch(ctx, rootID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("fresh claim dispatch: %v", err)
	}
	_ = newToken

	replay, err := teamStore.CompleteWorkflowTaskAttempt(ctx, oldAttempt, "late result")
	if err != nil {
		t.Fatalf("replay complete err=%v", err)
	}
	if replay.Outcome != store.WorkflowMutationStale {
		t.Fatalf("replay outcome=%v, want Stale", replay.Outcome)
	}
	if replay.ReadyToFinalize {
		t.Fatalf("Stale replay must not carry ReadyToFinalize=true; a superseded attempt must never arm finalize")
	}
}
