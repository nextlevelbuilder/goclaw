package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ── Plan validation ───────────────────────────────────────────────────────────
//
// Every refusal here is a workflow that would otherwise arm and then do nothing,
// or do something other than what is drawn. Silence is the failure mode being
// designed against.

func graphOf(t *testing.T, body string) *Graph {
	t.Helper()
	g, err := ParseGraph(json.RawMessage(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return g
}

const validGraph = `{
  "nodes":[
    {"id":"t1","type":"trigger","data":{"kind":"cron","expr":"0 8 * * 1","tz":"UTC"}},
    {"id":"a1","type":"agent","data":{"agentId":"11111111-1111-4111-8111-111111111111","prompt":"summarise competitors"}}
  ],
  "edges":[{"id":"e1","source":"t1","target":"a1"}]
}`

func TestPlanHappyPath(t *testing.T) {
	steps, err := graphOf(t, validGraph).Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	s := steps[0]
	if s.Kind != "cron" || s.Expr != "0 8 * * 1" || s.TZ != "UTC" {
		t.Errorf("schedule not carried: %+v", s)
	}
	if s.AgentID != "11111111-1111-4111-8111-111111111111" || s.Prompt != "summarise competitors" {
		t.Errorf("agent settings not carried: %+v", s)
	}
}

func TestPlanRefusals(t *testing.T) {
	cases := map[string]struct{ graph, wantMsg string }{
		"no trigger at all": {
			`{"nodes":[{"id":"a1","type":"agent","data":{"agentId":"55555555-5555-4555-8555-555555555555","prompt":"y"}}],"edges":[]}`,
			"nothing to run",
		},
		"trigger not connected": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}}],"edges":[]}`,
			"nothing to run",
		},
		"trigger wired to a non-agent": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
			           {"id":"c1","type":"capability","data":{}}],
			  "edges":[{"id":"e","source":"t1","target":"c1"}]}`,
			"must connect to an agent",
		},
		"agent not chosen": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
			           {"id":"a1","type":"agent","data":{"prompt":"do it"}}],
			  "edges":[{"id":"e","source":"t1","target":"a1"}]}`,
			"which agent",
		},
		"no prompt": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
			           {"id":"a1","type":"agent","data":{"agentId":"22222222-2222-4222-8222-222222222222"}}],
			  "edges":[{"id":"e","source":"t1","target":"a1"}]}`,
			"what the agent should do",
		},
		"trigger type missing": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{}},
			           {"id":"a1","type":"agent","data":{"agentId":"22222222-2222-4222-8222-222222222222","prompt":"p"}}],
			  "edges":[{"id":"e","source":"t1","target":"a1"}]}`,
			"choose a trigger type",
		},
		"cron with no expression": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron"}},
			           {"id":"a1","type":"agent","data":{"agentId":"22222222-2222-4222-8222-222222222222","prompt":"p"}}],
			  "edges":[{"id":"e","source":"t1","target":"a1"}]}`,
			"set a schedule",
		},
		"dangling edge": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}}],
			  "edges":[{"id":"e","source":"t1","target":"ghost"}]}`,
			"not in the graph",
		},
		// The cron store parses the agent id as a UUID and silently drops anything
		// else, so a graph carrying an agent_key would arm and then run with NO
		// agent attached. Refused here instead.
		"agent identified by key, not id": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
			           {"id":"a1","type":"agent","data":{"agentId":"researcher","prompt":"p"}}],
			  "edges":[{"id":"e","source":"t1","target":"a1"}]}`,
			"by its id, not its name",
		},
		// Named so a channel trigger drawn before the compiler supports it fails at
		// arm time rather than arming something that never fires.
		"unsupported trigger kind": {
			`{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"channel"}},
			           {"id":"a1","type":"agent","data":{"agentId":"22222222-2222-4222-8222-222222222222","prompt":"p"}}],
			  "edges":[{"id":"e","source":"t1","target":"a1"}]}`,
			"cannot be scheduled yet",
		},
	}
	for name, c := range cases {
		_, err := graphOf(t, c.graph).Plan()
		if err == nil {
			t.Errorf("%s: plan succeeded, want refusal", name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: error %q does not mention %q", name, err, c.wantMsg)
		}
	}
}

// A sub-minute repeat is nearly always a mistyped unit, and it would start an
// agent run every few seconds — expensive, and hard to stop once armed.
func TestPlanRejectsSubMinuteRepeat(t *testing.T) {
	tooOften := int64(5_000)
	fine := int64(time.Minute / time.Millisecond)
	mk := func(ms int64) string {
		b, _ := json.Marshal(ms)
		return `{"nodes":[{"id":"t1","type":"trigger","data":{"kind":"every","everyMs":` + string(b) + `}},
		          {"id":"a1","type":"agent","data":{"agentId":"22222222-2222-4222-8222-222222222222","prompt":"p"}}],
		  "edges":[{"id":"e","source":"t1","target":"a1"}]}`
	}
	if _, err := graphOf(t, mk(tooOften)).Plan(); err == nil {
		t.Error("a 5-second repeat was accepted")
	}
	if _, err := graphOf(t, mk(fine)).Plan(); err != nil {
		t.Errorf("a one-minute repeat was refused: %v", err)
	}
}

// Shapes the compiler does not understand yet (capability grants, agent→agent)
// must not make a graph unsavable — the canvas will always be ahead of this.
func TestPlanIgnoresNonTriggerEdges(t *testing.T) {
	g := graphOf(t, `{
	  "nodes":[
	    {"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
	    {"id":"a1","type":"agent","data":{"agentId":"22222222-2222-4222-8222-222222222222","prompt":"p"}},
	    {"id":"c1","type":"capability","data":{}}
	  ],
	  "edges":[
	    {"id":"e1","source":"t1","target":"a1"},
	    {"id":"e2","source":"c1","target":"a1"}
	  ]}`)
	steps, err := g.Plan()
	if err != nil {
		t.Fatalf("a capability edge broke the plan: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("got %d steps, want 1 (the capability edge must not compile)", len(steps))
	}
}

// ── Compile / retract ─────────────────────────────────────────────────────────

type fakeCron struct {
	store.CronStore
	added   []store.CronJob
	removed []string
	failOn  int // 1-based index of the AddJob call that should fail; 0 = never
	calls   int
}

func (f *fakeCron) AddJob(_ context.Context, name string, sch store.CronSchedule, msg string,
	deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	f.calls++
	if f.failOn == f.calls {
		return nil, errors.New("cron rejected the schedule")
	}
	job := store.CronJob{
		ID: "job-" + name, Name: name, Schedule: sch,
		Payload: store.CronPayload{Message: msg}, AgentID: agentID, UserID: userID,
		Deliver: deliver, DeliverChannel: channel, DeliverTo: to,
	}
	f.added = append(f.added, job)
	return &job, nil
}

func (f *fakeCron) RemoveJob(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}

type fakeWFStore struct {
	store.WorkflowStore
	compiled map[uuid.UUID]json.RawMessage
	errs     map[uuid.UUID]*string
	enabled  []*store.Workflow
}

func newFakeWFStore() *fakeWFStore {
	return &fakeWFStore{compiled: map[uuid.UUID]json.RawMessage{}, errs: map[uuid.UUID]*string{}}
}

func (f *fakeWFStore) SetCompileResult(_ context.Context, _, id uuid.UUID, compiled json.RawMessage, e *string) error {
	f.compiled[id] = compiled
	f.errs[id] = e
	return nil
}
func (f *fakeWFStore) ListEnabled(context.Context) ([]*store.Workflow, error) { return f.enabled, nil }

func armedWorkflow() *store.Workflow {
	return &store.Workflow{
		ID: uuid.New(), TenantID: uuid.New(), Name: "Digest",
		Enabled: true, Graph: json.RawMessage(validGraph),
	}
}

func TestApplyArmsAndRecordsJobIDs(t *testing.T) {
	cron := &fakeCron{}
	wfs := newFakeWFStore()
	c := NewCompiler(wfs, cron)
	w := armedWorkflow()

	if err := c.Apply(context.Background(), w); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cron.added) != 1 {
		t.Fatalf("created %d jobs, want 1", len(cron.added))
	}
	job := cron.added[0]
	if job.AgentID != "11111111-1111-4111-8111-111111111111" || job.Payload.Message != "summarise competitors" {
		t.Errorf("job does not match the graph: %+v", job)
	}
	// A workflow's runs belong to the workspace, not to whoever pressed Arm —
	// otherwise that person leaving orphans the schedule.
	if job.UserID != "" {
		t.Errorf("job was attributed to a user: %q", job.UserID)
	}
	if !strings.Contains(job.Name, "Digest") {
		t.Errorf("job name %q does not name its workflow", job.Name)
	}

	var rec Compiled
	if err := json.Unmarshal(wfs.compiled[w.ID], &rec); err != nil {
		t.Fatalf("compile record: %v", err)
	}
	if len(rec.CronJobIDs) != 1 || rec.CronJobIDs[0] != job.ID {
		t.Errorf("recorded ids %v do not match the created job %q", rec.CronJobIDs, job.ID)
	}
	if wfs.errs[w.ID] != nil {
		t.Errorf("a successful arm recorded an error: %v", *wfs.errs[w.ID])
	}
}

// THE safety property. Retract must run before validation, or breaking a
// workflow leaves the previous version's schedule firing while the UI shows an
// error — the user believes it is stopped and it is not.
func TestApplyRetractsEvenWhenTheNewGraphIsInvalid(t *testing.T) {
	cron := &fakeCron{}
	wfs := newFakeWFStore()
	c := NewCompiler(wfs, cron)

	w := &store.Workflow{
		ID: uuid.New(), TenantID: uuid.New(), Name: "Broken", Enabled: true,
		Compiled: json.RawMessage(`{"cron_ids":["old-1","old-2"]}`),
		Graph:    json.RawMessage(`{"nodes":[],"edges":[]}`), // compiles to nothing
	}
	if err := c.Apply(context.Background(), w); err != nil {
		t.Fatalf("apply returned a transport error: %v", err)
	}
	if len(cron.removed) != 2 || cron.removed[0] != "old-1" {
		t.Errorf("previous jobs were not retracted: %v", cron.removed)
	}
	if wfs.errs[w.ID] == nil {
		t.Error("an invalid graph did not record a compile error")
	}
	// And the record must no longer claim to own jobs that were just removed.
	var rec Compiled
	_ = json.Unmarshal(wfs.compiled[w.ID], &rec)
	if len(rec.CronJobIDs) != 0 {
		t.Errorf("compile record still lists %v after a failed arm", rec.CronJobIDs)
	}
}

func TestDisarmRemovesJobsAndClearsError(t *testing.T) {
	cron := &fakeCron{}
	wfs := newFakeWFStore()
	c := NewCompiler(wfs, cron)

	boom := "an earlier failure"
	w := &store.Workflow{
		ID: uuid.New(), TenantID: uuid.New(), Name: "Off", Enabled: false,
		Compiled: json.RawMessage(`{"cron_ids":["j1"]}`), CompileError: &boom,
	}
	if err := c.Apply(context.Background(), w); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cron.removed) != 1 || cron.removed[0] != "j1" {
		t.Errorf("disarm did not remove the job: %v", cron.removed)
	}
	if wfs.errs[w.ID] != nil {
		t.Errorf("disarming kept a stale compile error: %v", *wfs.errs[w.ID])
	}
	if len(cron.added) != 0 {
		t.Error("disarming created jobs")
	}
}

// Half a workflow firing is worse than none of it, because the half that runs
// looks like success.
func TestPartialFailureRollsBack(t *testing.T) {
	cron := &fakeCron{failOn: 2}
	wfs := newFakeWFStore()
	c := NewCompiler(wfs, cron)

	twoSteps := `{
	  "nodes":[
	    {"id":"t1","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
	    {"id":"a1","type":"agent","data":{"agentId":"33333333-3333-4333-8333-333333333333","prompt":"p1"}},
	    {"id":"t2","type":"trigger","data":{"kind":"cron","expr":"* * * * *"}},
	    {"id":"a2","type":"agent","data":{"agentId":"44444444-4444-4444-8444-444444444444","prompt":"p2"}}
	  ],
	  "edges":[{"id":"e1","source":"t1","target":"a1"},{"id":"e2","source":"t2","target":"a2"}]}`
	w := &store.Workflow{ID: uuid.New(), TenantID: uuid.New(), Name: "Two", Enabled: true,
		Graph: json.RawMessage(twoSteps)}

	if err := c.Apply(context.Background(), w); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cron.added) != 1 {
		t.Fatalf("expected exactly one job to have been created before the failure, got %d", len(cron.added))
	}
	if len(cron.removed) != 1 || cron.removed[0] != cron.added[0].ID {
		t.Errorf("the successfully created job was not rolled back: removed=%v added=%v", cron.removed, cron.added[0].ID)
	}
	if wfs.errs[w.ID] == nil || !strings.Contains(*wfs.errs[w.ID], "step 2") {
		t.Errorf("compile error does not name the failing step: %v", wfs.errs[w.ID])
	}
}

// Reconcile runs on startup and must converge, not duplicate: each Apply
// retracts the recorded ids first.
func TestReconcileIsIdempotent(t *testing.T) {
	cron := &fakeCron{}
	wfs := newFakeWFStore()
	c := NewCompiler(wfs, cron)
	w := armedWorkflow()
	wfs.enabled = []*store.Workflow{w}

	c.ReconcileAll(context.Background())
	firstID := cron.added[0].ID

	c.ReconcileAll(context.Background())
	if len(cron.added) != 2 {
		t.Fatalf("second reconcile created %d jobs total, want 2 (one per pass)", len(cron.added))
	}
	// The point: the first pass's job was removed before the second was created,
	// so the live set is still exactly one.
	if len(cron.removed) != 1 || cron.removed[0] != firstID {
		t.Errorf("the previous job was not retracted on the second pass: %v", cron.removed)
	}
}

// A corrupt record means the jobs cannot be named. Better to leave them and log
// than to guess — but the disarm itself must still complete.
func TestUnreadableCompileRecordDoesNotBlockDisarm(t *testing.T) {
	cron := &fakeCron{}
	wfs := newFakeWFStore()
	c := NewCompiler(wfs, cron)

	w := &store.Workflow{ID: uuid.New(), TenantID: uuid.New(), Name: "Corrupt",
		Enabled: false, Compiled: json.RawMessage(`not json`)}
	if err := c.Apply(context.Background(), w); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cron.removed) != 0 {
		t.Errorf("removed jobs it could not have known about: %v", cron.removed)
	}
	if wfs.compiled[w.ID] == nil {
		t.Error("compile record was not reset")
	}
}

func TestCompilerUnavailable(t *testing.T) {
	var nilCompiler *Compiler
	if nilCompiler.Available() {
		t.Error("a nil compiler reported itself available")
	}
	c := NewCompiler(nil, nil)
	if c.Available() {
		t.Error("a compiler with no stores reported itself available")
	}
	if err := c.Apply(context.Background(), armedWorkflow()); err == nil {
		t.Error("apply on an unavailable compiler silently succeeded")
	}
}
