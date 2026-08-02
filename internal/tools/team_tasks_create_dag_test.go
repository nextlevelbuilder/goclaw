package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// recordingWorkflowStore is a test double for the durable workflow store. It
// embeds baseNoopTeamStore so it satisfies store.TeamStore, embeds the
// store.TeamWorkflowStore interface so the full 36-method set compiles, and
// overrides only CreateWorkflowWithTasks — the single method create_dag calls.
// Unused workflow methods are promoted from the embedded (nil) interface and
// would panic if reached; the tests never reach them.
type recordingWorkflowStore struct {
	baseNoopTeamStore
	store.TeamWorkflowStore

	mu           sync.Mutex
	createCalls  int
	lastWorkflow *store.TeamWorkflowData
	lastTasks    []store.TeamTaskData
	createErr    error
}

func (s *recordingWorkflowStore) CreateWorkflowWithTasks(_ context.Context, workflow *store.TeamWorkflowData, tasks []store.TeamTaskData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	s.lastWorkflow = workflow
	s.lastTasks = tasks
	return nil
}

// dagTestBackend wraps mockBackend so Store() returns the recording workflow
// store, letting executeCreateDAG's workflowStore() assertion succeed while
// every other backend behavior (roster, resolve, broadcast) stays mocked.
type dagTestBackend struct {
	*mockBackend
	wfStore *recordingWorkflowStore
}

func (b *dagTestBackend) Store() store.TeamStore { return b.wfStore }

var _ store.TeamWorkflowStore = (*recordingWorkflowStore)(nil)
var _ TeamToolBackend = (*dagTestBackend)(nil)

// newDAGSetup builds a lead-run context with a valid run id, an accepted
// classification audit id, and a PendingTeamDispatch container on the run — the
// preconditions create_dag requires before parsing, plus the dispatch sink tests
// inspect. Callers override ctx (agent id, channel, audit) per case and read the
// dispatch container back via PendingTeamDispatchFromCtx(ctx).
func newDAGSetup(auditID uuid.UUID) (*recordingWorkflowStore, *TeamTasksTool, context.Context) {
	mb, _, _, _, ctx := newTestTeamSetup()
	wf := &recordingWorkflowStore{}
	tool := NewTeamTasksTool(&dagTestBackend{mockBackend: mb, wfStore: wf}, FullTeamPolicy{})
	ctx = WithToolRunID(ctx, "run-dag-1")
	ctx = WithPendingTeamDispatch(ctx, NewPendingTeamDispatch())
	if auditID != uuid.Nil {
		ctx = WithTeamWorkClassificationAuditID(ctx, auditID)
	}
	return wf, tool, ctx
}

// dagTask builds one well-formed task object for a create_dag call.
func dagTask(id, subject, description, assignee string, blockedBy []any, terminal bool) map[string]any {
	return map[string]any{
		"id":          id,
		"subject":     subject,
		"description": description,
		"assignee":    assignee,
		"blocked_by":  blockedBy,
		"terminal":    terminal,
	}
}

func TestCreateDAGValidLeadRequest(t *testing.T) {
	auditID := uuid.New()
	wf, tool, ctx := newDAGSetup(auditID)

	tasks := []any{
		dagTask("a", "Research", "Gather inputs; output a summary", "member-agent", []any{}, false),
		dagTask("b", "Integrate", "Combine a's summary into the final answer", "lead-agent", []any{"a"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.ForLLM)
	}
	if wf.createCalls != 1 {
		t.Fatalf("expected exactly one store write, got %d", wf.createCalls)
	}
	if len(wf.lastTasks) != 2 {
		t.Fatalf("expected 2 persisted tasks, got %d", len(wf.lastTasks))
	}
	if wf.lastWorkflow.ClassificationAuditID == nil || *wf.lastWorkflow.ClassificationAuditID != auditID {
		t.Fatalf("expected the exact audit id %s persisted, got %v", auditID, wf.lastWorkflow.ClassificationAuditID)
	}
	// a is a root (pending), b depends on a (blocked) and is the terminal task.
	if wf.lastTasks[0].Status != store.TeamTaskStatusPending {
		t.Errorf("root task a: expected pending, got %q", wf.lastTasks[0].Status)
	}
	if wf.lastTasks[1].Status != store.TeamTaskStatusBlocked {
		t.Errorf("dependent task b: expected blocked, got %q", wf.lastTasks[1].Status)
	}
	if !wf.lastTasks[1].WorkflowTerminal {
		t.Error("task b must be marked terminal")
	}
	// The dependent task's blocked_by must resolve to the root task's real UUID
	// (the local "a" label is mapped to a UUID before persistence).
	rootID := wf.lastTasks[0].ID
	if len(wf.lastTasks[1].BlockedBy) != 1 || wf.lastTasks[1].BlockedBy[0] != rootID {
		t.Errorf("task b blocked_by must resolve to root UUID %s, got %v", rootID, wf.lastTasks[1].BlockedBy)
	}
	if wf.lastWorkflow.TerminalTaskID != nil {
		// TerminalTaskID is sealed by the store layer; create_dag leaves it for
		// CreateWorkflowWithTasks. Guard against accidentally pre-setting it here.
		t.Errorf("create_dag must not pre-seal TerminalTaskID, got %v", wf.lastWorkflow.TerminalTaskID)
	}
	// Dispatch after the seal: only the root (pending) task is queued for the
	// post-turn drain; the blocked terminal task is not.
	ptd := PendingTeamDispatchFromCtx(ctx)
	if ptd == nil {
		t.Fatal("expected a PendingTeamDispatch on the run context")
	}
	queued := ptd.Drain()
	queuedIDs := queued[testTeamID]
	if len(queuedIDs) != 1 || queuedIDs[0] != rootID {
		t.Errorf("only the root task %s should be queued after the store returns, got %v", rootID, queuedIDs)
	}
}

func TestCreateDAGTerminalMayBeMember(t *testing.T) {
	wf, tool, ctx := newDAGSetup(uuid.New())
	tasks := []any{
		dagTask("a", "Draft", "Write the draft", "member-agent", []any{}, false),
		dagTask("b", "Integrate", "Final synthesis", "member2-agent", []any{"a"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if result.IsError {
		t.Fatalf("member-owned terminal should be allowed, got: %v", result.ForLLM)
	}
	if wf.createCalls != 1 {
		t.Fatalf("expected one write, got %d", wf.createCalls)
	}
}

func TestCreateDAGDeniesNonLead(t *testing.T) {
	// A member must never author a DAG. Critically, a spoofed teammate/system
	// channel must NOT widen access the way RequireLead's bypass would — the check
	// is agentID == team.LeadAgentID, independent of channel.
	for _, channel := range []string{ChannelTeammate, ChannelSystem, ChannelDashboard} {
		t.Run(channel, func(t *testing.T) {
			wf, tool, ctx := newDAGSetup(uuid.New())
			ctx = store.WithAgentID(ctx, testMemberID) // not the lead
			ctx = WithToolChannel(ctx, channel)
			result := tool.Execute(ctx, map[string]any{
				"action": "create_dag",
				"tasks":  []any{dagTask("a", "S", "D", "member-agent", []any{}, false)},
			})
			if !result.IsError {
				t.Fatalf("expected member to be denied on channel %q", channel)
			}
			if !strings.Contains(result.ForLLM, "team lead") {
				t.Errorf("expected lead-required error, got: %v", result.ForLLM)
			}
			if wf.createCalls != 0 {
				t.Errorf("denied request must write nothing, got %d writes", wf.createCalls)
			}
		})
	}
}

func TestCreateDAGDeniesMissingAudit(t *testing.T) {
	// Outside an accepted classification gate there is no audit id: create_dag must
	// fail closed with zero writes.
	wf, tool, ctx := newDAGSetup(uuid.Nil) // no audit id on ctx
	result := tool.Execute(ctx, map[string]any{
		"action": "create_dag",
		"tasks": []any{
			dagTask("a", "S", "D", "member-agent", []any{}, false),
			dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true),
		},
	})
	if !result.IsError {
		t.Fatal("expected missing-audit denial")
	}
	if !strings.Contains(result.ForLLM, "classification") {
		t.Errorf("expected classification-gate error, got: %v", result.ForLLM)
	}
	if wf.createCalls != 0 {
		t.Errorf("missing-audit denial must write nothing, got %d writes", wf.createCalls)
	}
}

func TestCreateDAGDeniesLeadOnNonTerminal(t *testing.T) {
	wf, tool, ctx := newDAGSetup(uuid.New())
	tasks := []any{
		dagTask("a", "Work", "Non-terminal work assigned to the lead", "lead-agent", []any{}, false),
		dagTask("b", "Integrate", "Synthesis", "member-agent", []any{"a"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if !result.IsError {
		t.Fatal("lead-owned non-terminal task must be rejected")
	}
	if !strings.Contains(result.ForLLM, "non-terminal") {
		t.Errorf("expected non-terminal lead error, got: %v", result.ForLLM)
	}
	if wf.createCalls != 0 {
		t.Errorf("rejected DAG must write nothing, got %d", wf.createCalls)
	}
}

func TestCreateDAGDeniesUnknownAssignee(t *testing.T) {
	wf, tool, ctx := newDAGSetup(uuid.New())
	tasks := []any{
		dagTask("a", "Work", "Assigned to a ghost", "not-a-real-agent", []any{}, false),
		dagTask("b", "Integrate", "Synthesis", "lead-agent", []any{"a"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if !result.IsError {
		t.Fatal("unknown assignee must be rejected")
	}
	if wf.createCalls != 0 {
		t.Errorf("bad-assignee DAG must write nothing, got %d", wf.createCalls)
	}
}

func TestCreateDAGRejectsMalformedFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(m map[string]any)
		errPart string
	}{
		{"missing id", func(m map[string]any) { delete(m, "id") }, "id"},
		{"missing subject", func(m map[string]any) { delete(m, "subject") }, "subject"},
		{"missing description", func(m map[string]any) { delete(m, "description") }, "description"},
		{"missing assignee", func(m map[string]any) { delete(m, "assignee") }, "assignee"},
		{"missing terminal", func(m map[string]any) { delete(m, "terminal") }, "terminal"},
		{"missing blocked_by", func(m map[string]any) { delete(m, "blocked_by") }, "blocked_by"},
		{"null blocked_by", func(m map[string]any) { m["blocked_by"] = nil }, "blocked_by"},
		{"string blocked_by", func(m map[string]any) { m["blocked_by"] = "a" }, "blocked_by"},
		{"numeric blocked_by entry", func(m map[string]any) { m["blocked_by"] = []any{5} }, "blocked_by"},
		{"empty dep id", func(m map[string]any) { m["blocked_by"] = []any{""} }, "empty dependency"},
		{"string terminal", func(m map[string]any) { m["terminal"] = "true" }, "boolean"},
		{"numeric id", func(m map[string]any) { m["id"] = 7 }, "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf, tool, ctx := newDAGSetup(uuid.New())
			bad := dagTask("a", "S", "D", "member-agent", []any{}, false)
			tc.mutate(bad)
			// A malformed first task fails in the parser regardless of the second
			// task, which just satisfies the >=2 task precondition on the valid path.
			tasks := []any{bad, dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true)}
			result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
			if !result.IsError {
				t.Fatalf("expected rejection for %s", tc.name)
			}
			if !strings.Contains(result.ForLLM, tc.errPart) {
				t.Errorf("expected error containing %q, got: %v", tc.errPart, result.ForLLM)
			}
			if wf.createCalls != 0 {
				t.Errorf("malformed DAG must write nothing, got %d", wf.createCalls)
			}
		})
	}
}

func TestCreateDAGGraphValidation(t *testing.T) {
	cases := []struct {
		name    string
		tasks   []any
		errPart string
	}{
		{
			"single task",
			[]any{dagTask("a", "S", "D", "member-agent", []any{}, true)},
			"at least 2",
		},
		{
			"duplicate id",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				dagTask("a", "S2", "D2", "lead-agent", []any{}, true),
			},
			"duplicate task id",
		},
		{
			"unknown dep",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				dagTask("b", "S2", "D2", "lead-agent", []any{"zzz"}, true),
			},
			"unknown task",
		},
		{
			"self block",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{"a"}, false),
				dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true),
			},
			"blocks on itself",
		},
		{
			"duplicate dep",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				dagTask("b", "S2", "D2", "lead-agent", []any{"a", "a"}, true),
			},
			"twice",
		},
		{
			"cycle",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{"b"}, false),
				dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true),
			},
			"cycle",
		},
		{
			"no terminal",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, false),
			},
			"none was marked",
		},
		{
			"multiple terminals",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, true),
				dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true),
			},
			"more than one",
		},
		{
			"dead-end non-terminal sink",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true),
				dagTask("c", "S3", "D3", "member2-agent", []any{}, false),
			},
			"no dependent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf, tool, ctx := newDAGSetup(uuid.New())
			result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tc.tasks})
			if !result.IsError {
				t.Fatalf("expected rejection for %s", tc.name)
			}
			if !strings.Contains(result.ForLLM, tc.errPart) {
				t.Errorf("expected error containing %q, got: %v", tc.errPart, result.ForLLM)
			}
			if wf.createCalls != 0 {
				t.Errorf("invalid DAG must write nothing, got %d", wf.createCalls)
			}
		})
	}
}

func TestCreateDAGStoreErrorSurfaces(t *testing.T) {
	wf, tool, ctx := newDAGSetup(uuid.New())
	wf.createErr = errors.New("boom")
	tasks := []any{
		dagTask("a", "S", "D", "member-agent", []any{}, false),
		dagTask("b", "S2", "D2", "lead-agent", []any{"a"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if !result.IsError {
		t.Fatal("expected store error to surface")
	}
	if !strings.Contains(result.ForLLM, "failed to create workflow DAG") {
		t.Errorf("expected store-failure message, got: %v", result.ForLLM)
	}
	// A failed store write must not queue any roots for dispatch.
	ptd := PendingTeamDispatchFromCtx(ctx)
	if ptd == nil {
		t.Fatal("expected a PendingTeamDispatch on the run context")
	}
	if queued := ptd.Drain(); len(queued) != 0 {
		t.Errorf("store error must queue no roots, got %v", queued)
	}
}

func TestCreateDAGAllowedByBothEditionPolicies(t *testing.T) {
	// create_dag must be advertised AND authorized by both editions: the policy is
	// membership-based, so IsAllowed and the Schema enum (AllowedActions) agree.
	for name, policy := range map[string]TeamActionPolicy{
		"full": FullTeamPolicy{},
		"lite": LiteTeamPolicy{},
	} {
		t.Run(name, func(t *testing.T) {
			if !policy.IsAllowed("create_dag") {
				t.Errorf("%s policy must allow create_dag", name)
			}
			if !actionAllowed(policy.AllowedActions(), "create_dag") {
				t.Errorf("%s policy AllowedActions must advertise create_dag", name)
			}
			// An action outside the contract stays denied in both editions.
			if policy.IsAllowed("nope") || actionAllowed(policy.AllowedActions(), "nope") {
				t.Errorf("%s policy must not allow an unknown action", name)
			}
		})
	}
}

// TestCreateDAGSchemaDeclaresTasksProperty pins the fix for the schema gap that
// caused the live coordinated-lead loop: create_dag's required `tasks` array was
// documented ONLY in the `action` prose, NOT as a declared JSON schema property,
// so the lead model flattened create_dag fields (assignee, description,
// blocked_by) as top-level args and parseDAGTasks rejected the call 6×. The
// provider-facing schema is Parameters() passed verbatim (ToProviderDef /
// Registry.ProviderDefs add no additionalProperties:false), so the `tasks`
// slot MUST exist structurally for the model to place the graph there. This test
// guards against the property silently disappearing again.
func TestCreateDAGSchemaDeclaresTasksProperty(t *testing.T) {
	// The `tasks` property is declared unconditionally in Parameters(); create_dag
	// is only advertised in the full edition's enum, so the schema is asserted
	// under FullTeamPolicy (the edition where create_dag is authorable).
	_, tool, _, _, _ := newTestTeamSetup()
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Parameters() has no properties map")
	}
	tasksProp, ok := props["tasks"]
	if !ok {
		t.Fatalf("Parameters() must declare a `tasks` property for create_dag — without it the provider schema has no slot for the DAG and the model flattens fields as top-level args (the live coordinated-lead loop)")
	}
	tasksMap, ok := tasksProp.(map[string]any)
	if !ok {
		t.Fatalf("`tasks` property must be a JSON schema object, got %T", tasksProp)
	}
	if tasksMap["type"] != "array" {
		t.Errorf("`tasks` must be type=array, got %v", tasksMap["type"])
	}
	items, ok := tasksMap["items"].(map[string]any)
	if !ok {
		t.Fatalf("`tasks.items` must be an object schema describing each DAG node, got %T", tasksMap["items"])
	}
	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("`tasks.items` must declare properties for each DAG node field")
	}
	// Every field parseDAGTasks reads (id, subject, description, assignee,
	// blocked_by, terminal) must be declared so the model has a structural
	// definition of the node — not just a prose hint it can ignore.
	for _, field := range []string{"id", "subject", "description", "assignee", "blocked_by", "terminal"} {
		if _, ok := itemProps[field]; !ok {
			t.Errorf("`tasks.items` must declare property %q (parseDAGTasks reads it)", field)
		}
	}
	required, ok := items["required"].([]string)
	if !ok {
		t.Fatalf("`tasks.items` must mark all node fields required (parser is fail-closed on any missing field), got %T", items["required"])
	}
	requiredSet := make(map[string]bool, len(required))
	for _, r := range required {
		requiredSet[r] = true
	}
	for _, field := range []string{"id", "subject", "description", "assignee", "blocked_by", "terminal"} {
		if !requiredSet[field] {
			t.Errorf("`tasks.items` must mark %q as required (a dropped field must fail the batch, not stringify to <nil>)", field)
		}
	}
	// The `action` enum must still advertise create_dag so the model
	// knows the action exists alongside the structural `tasks` slot.
	actionProp, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("`action` property missing")
	}
	enum, _ := actionProp["enum"].([]string)
	if !actionAllowed(enum, "create_dag") {
		t.Errorf("`action` enum must still advertise create_dag")
	}
}

// TestLeadTaskAutoFailsPredicate pins the narrowed dispatch guard that lets the
// canonical lead run ONLY a terminal workflow-work task (a coordinator DAG
// integration step) while auto-failing every other lead-owned task. This is the
// runtime half of the create_dag contract: without the terminal exception the
// lead-owned integration task could never dispatch; without the rest of the guard
// the lead could self-dispatch an ordinary or non-terminal task and loop.
func TestLeadTaskAutoFailsPredicate(t *testing.T) {
	leadID := uuid.New()
	memberID := uuid.New()

	terminalWork := &store.TeamTaskData{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true}
	nonTerminalWork := &store.TeamTaskData{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: false}
	plain := &store.TeamTaskData{WorkflowKind: "", WorkflowTerminal: false}

	cases := []struct {
		name   string
		owner  uuid.UUID
		task   *store.TeamTaskData
		wantAF bool
	}{
		// The single exception: lead-owned terminal workflow work dispatches.
		{"lead terminal workflow-work dispatches", leadID, terminalWork, false},
		// Everything else owned by the lead auto-fails.
		{"lead non-terminal workflow-work auto-fails", leadID, nonTerminalWork, true},
		{"lead plain task auto-fails", leadID, plain, true},
		// Non-lead owners are never affected by the lead guard at all.
		{"member terminal workflow-work dispatches", memberID, terminalWork, false},
		{"member plain task dispatches", memberID, plain, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := leadTaskAutoFails(tc.owner, leadID, tc.task); got != tc.wantAF {
				t.Errorf("leadTaskAutoFails(%s) = %v, want %v", tc.name, got, tc.wantAF)
			}
		})
	}
}

// TestCreateDAGOriginRoutingCarriesLocaleAndBooleanMarker proves G9 historical
// compatibility: a coordinator DAG persists OriginRouting as a JSON object that
// keeps the coordinator_dag marker as a BOOLEAN true (not a string) AND threads
// the requester locale so a failed/cancelled DAG's deterministic summary renders
// in the right language. workflowLocale (cmd/gateway_workflow_finalize.go) reads
// the locale key from OriginRouting before falling back to Vietnamese-detection
// on the canonical plan — which is a flat task array for DAGs and yields no
// prose, so without this threading the locale would always fall back to "en".
func TestCreateDAGOriginRoutingCarriesLocaleAndBooleanMarker(t *testing.T) {
	auditID := uuid.New()
	wf, tool, ctx := newDAGSetup(auditID)
	ctx = store.WithLocale(ctx, "vi")

	tasks := []any{
		dagTask("a", "Research", "Gather inputs", "member-agent", []any{}, false),
		dagTask("b", "Integrate", "Combine into final answer", "lead-agent", []any{"a"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.ForLLM)
	}

	var routing map[string]any
	if err := json.Unmarshal(wf.lastWorkflow.OriginRouting, &routing); err != nil {
		t.Fatalf("OriginRouting is not a JSON object: %v (raw=%s)", err, wf.lastWorkflow.OriginRouting)
	}
	// coordinator_dag must remain a BOOLEAN true — downstream raw-marker consumers
	// (team_tool_dispatch.go MetaOriginRouting) read the routing string verbatim,
	// and changing the type would silently invert boolean checks elsewhere.
	marker, ok := routing["coordinator_dag"].(bool)
	if !ok || !marker {
		t.Fatalf("coordinator_dag must be boolean true, got %T=%v", routing["coordinator_dag"], routing["coordinator_dag"])
	}
	// The requester locale must be threaded so failed/cancelled DAG summaries
	// render in the right language.
	if routing["locale"] != "vi" {
		t.Fatalf("locale = %v, want vi", routing["locale"])
	}
}

// withReviews attaches an explicit review list to a dagTask built by dagTask.
func withReviews(task map[string]any, reviews ...any) map[string]any {
	task["reviews"] = reviews
	return task
}

func TestCreateDAGAcceptsValidIndependentReview(t *testing.T) {
	auditID := uuid.New()
	wf, tool, ctx := newDAGSetup(auditID)
	ctx = WithTeamWorkReviewRequired(ctx, true)

	tasks := []any{
		dagTask("a", "Research", "Gather inputs", "member-agent", []any{}, false),
		withReviews(dagTask("c", "Review", "Independently critique a", "member2-agent", []any{"a"}, false), "a"),
		dagTask("t", "Integrate", "Final synthesis", "lead-agent", []any{"c"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if result.IsError {
		t.Fatalf("valid review DAG rejected: %v", result.ForLLM)
	}
	if wf.createCalls != 1 || len(wf.lastTasks) != 3 {
		t.Fatalf("expected 3 persisted tasks in one write, got calls=%d tasks=%d", wf.createCalls, len(wf.lastTasks))
	}
	var critic, producer *store.TeamTaskData
	for i := range wf.lastTasks {
		switch wf.lastTasks[i].WorkflowStepID {
		case "c":
			critic = &wf.lastTasks[i]
		case "a":
			producer = &wf.lastTasks[i]
		}
	}
	if critic == nil || producer == nil {
		t.Fatalf("expected critic and producer tasks, got %+v", wf.lastTasks)
	}
	reviews, ok := critic.Metadata["reviews"].([]string)
	if !ok || len(reviews) != 1 || reviews[0] != "a" {
		t.Errorf("critic metadata reviews = %v, want [a]", critic.Metadata["reviews"])
	}
	if _, has := producer.Metadata["reviews"]; has {
		t.Errorf("producer must not carry reviews metadata: %+v", producer.Metadata)
	}
	if !strings.Contains(string(wf.lastWorkflow.CanonicalPlan), `"reviews":["a"]`) {
		t.Errorf("canonical plan must persist reviews, got %s", wf.lastWorkflow.CanonicalPlan)
	}
}

func TestCreateDAGReviewViolations(t *testing.T) {
	cases := []struct {
		name    string
		tasks   []any
		errPart string
		noFlag  bool // when true, run without WithTeamWorkReviewRequired
	}{
		{
			"no review task when required",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				dagTask("t", "S2", "D2", "lead-agent", []any{"a"}, true),
			},
			"review_required was set",
			false,
		},
		{
			"terminal declares reviews",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("t", "S2", "D2", "lead-agent", []any{"a"}, true), "a"),
			},
			"a critic cannot be the integration task",
			false,
		},
		{
			"reviewer owns the work it reviews",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member-agent", []any{"a"}, false), "a"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"c"}, true),
			},
			"independent reviewer cannot be the author",
			false,
		},
		{
			"reviews unknown task",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member2-agent", []any{"a"}, false), "zzz"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"c"}, true),
			},
			"not a non-terminal work task",
			false,
		},
		{
			"reviews the terminal task",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member2-agent", []any{"a"}, false), "t"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"c"}, true),
			},
			"not a non-terminal work task",
			false,
		},
		{
			"reviews itself",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member2-agent", []any{"a"}, false), "c"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"c"}, true),
			},
			"cannot review itself",
			false,
		},
		{
			"duplicate review id",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member2-agent", []any{"a"}, false), "a", "a"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"c"}, true),
			},
			"more than once",
			false,
		},
		{
			"reviewed producer does not reach the review",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member2-agent", []any{}, false), "a"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"a", "c"}, true),
			},
			"does not precede it in the graph",
			false,
		},
		{
			"review task declared when not required",
			[]any{
				dagTask("a", "S", "D", "member-agent", []any{}, false),
				withReviews(dagTask("c", "S2", "D2", "member2-agent", []any{"a"}, false), "a"),
				dagTask("t", "S3", "D3", "lead-agent", []any{"c"}, true),
			},
			"review_required is false",
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf, tool, ctx := newDAGSetup(uuid.New())
			if !tc.noFlag {
				ctx = WithTeamWorkReviewRequired(ctx, true)
			}
			result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tc.tasks})
			if !result.IsError {
				t.Fatalf("expected rejection for %s", tc.name)
			}
			if !strings.Contains(result.ForLLM, tc.errPart) {
				t.Errorf("expected error containing %q, got: %v", tc.errPart, result.ForLLM)
			}
			if wf.createCalls != 0 {
				t.Errorf("rejected DAG must write nothing, got %d", wf.createCalls)
			}
		})
	}
}

// R3 is defense-in-depth: executeCreateDAG's graph validation already makes the
// terminal the unique reachable sink, so a review that cannot reach it is only
// constructible by calling the validator directly.
func TestValidateReviewTasksRequiresReachableTerminal(t *testing.T) {
	inputs := []dagTaskInput{
		{ID: "a", Assignee: "m1"},
		{ID: "c", Assignee: "m2", BlockedBy: []string{"a"}, Reviews: []string{"a"}},
		{ID: "t", Terminal: true},
	}
	ownerIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	err := validateReviewTasks(inputs, ownerIDs, false)
	if err == nil || !strings.Contains(err.Error(), "does not reach the terminal") {
		t.Fatalf("err=%v, want review→terminal reachability rejection", err)
	}
}

func TestCreateDAGReviewForbiddenWhenNotRequired(t *testing.T) {
	wf, tool, ctx := newDAGSetup(uuid.New())
	// No WithTeamWorkReviewRequired — a voluntary review task must be rejected.
	tasks := []any{
		dagTask("a", "Research", "Gather inputs", "member-agent", []any{}, false),
		withReviews(dagTask("c", "Review", "Critique a", "member2-agent", []any{"a"}, false), "a"),
		dagTask("t", "Integrate", "Synthesis", "lead-agent", []any{"c"}, true),
	}
	result := tool.Execute(ctx, map[string]any{"action": "create_dag", "tasks": tasks})
	if !result.IsError {
		t.Fatalf("review task when review_required=false must be rejected")
	}
	if !strings.Contains(result.ForLLM, "review_required is false") {
		t.Errorf("expected error containing %q, got: %v", "review_required is false", result.ForLLM)
	}
	if wf.createCalls != 0 {
		t.Fatalf("rejected DAG must write nothing, got %d", wf.createCalls)
	}
}
