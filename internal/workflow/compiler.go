package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compiled is what a successful compile produced, stored on the workflow row.
//
// It exists so retraction works by RECORDED ID rather than by re-deriving from
// the current graph. Re-deriving is how orphaned schedules happen: disarm a
// workflow after editing it and the derivation no longer names the jobs the
// previous arm created, which then fire forever with nobody to explain them.
type Compiled struct {
	CronJobIDs []string `json:"cron_ids,omitempty"`
	// AgentLinkIDs are the delegation links this compile CREATED, so retraction
	// removes exactly those. Links the user made themselves must survive a disarm,
	// which is why they are recorded rather than derived from the graph.
	AgentLinkIDs []string `json:"agent_link_ids,omitempty"`
}

// Compiler arms and disarms workflows by creating and removing cron jobs.
type Compiler struct {
	workflows store.WorkflowStore
	cron      store.CronStore
	// links permits agent→agent handoffs. Nil-safe: without it a chain refuses to
	// arm rather than arming a handoff the delegate tool would then deny — a
	// workflow that runs its first step and stops is harder to diagnose than one
	// that never arms.
	links store.AgentLinkStore
}

func NewCompiler(workflows store.WorkflowStore, cron store.CronStore) *Compiler {
	return &Compiler{workflows: workflows, cron: cron}
}

// WithLinks enables agent→agent handoffs.
func (c *Compiler) WithLinks(links store.AgentLinkStore) *Compiler {
	c.links = links
	return c
}

// Available reports whether compilation can happen at all on this deployment.
func (c *Compiler) Available() bool {
	return c != nil && c.workflows != nil && c.cron != nil
}

// Apply reconciles one workflow with reality: it retracts whatever the last
// compile created, then — if the workflow is armed — creates the new jobs and
// records their ids.
//
// RETRACT ALWAYS RUNS FIRST, including when the new plan is invalid. The
// alternative (validate, then retract) leaves the old schedule live while the UI
// reports a failure, so a user who breaks their workflow keeps getting runs from
// the version they thought they had replaced.
func (c *Compiler) Apply(ctx context.Context, w *store.Workflow) error {
	if !c.Available() {
		return fmt.Errorf("workflow compilation is not available on this deployment")
	}

	// Scope every cron operation to the WORKFLOW's tenant.
	//
	// Load-bearing, and not obvious. The cron store is tenant-scoped from context,
	// and the two halves disagree when it is absent: inserts fall back to the
	// MASTER tenant (tenantIDForInsert) while reads do not (scanJob filters on the
	// raw context value). ReconcileAll runs on context.Background(), so without
	// this every workflow's job was written into the master tenant and then could
	// not be read back — observed on staging as "inserted but could not be read
	// back", with the row left behind as an orphan.
	//
	// Deriving the tenant from the row rather than the caller also means a job
	// always belongs to the workflow that created it, whichever path armed it.
	ctx = store.WithTenantID(ctx, w.TenantID)

	c.retract(ctx, w)

	if !w.Enabled {
		// Disarmed: retraction was the whole job. Clear compile state so the UI does
		// not show an error from a previous attempt against a workflow that is now
		// simply off.
		return c.record(ctx, w, Compiled{}, nil)
	}

	graph, err := ParseGraph(w.Graph)
	if err != nil {
		return c.failed(ctx, w, err)
	}
	steps, err := graph.Plan()
	if err != nil {
		return c.failed(ctx, w, err)
	}

	var created Compiled
	for i, s := range steps {
		// Permit each handoff before scheduling anything. A chain whose links cannot
		// be created must not arm: the first agent would run, try to delegate, be
		// refused by the delegate tool, and the workflow would half-happen.
		linkIDs, err := c.ensureLinks(ctx, w, s)
		created.AgentLinkIDs = append(created.AgentLinkIDs, linkIDs...)
		if err != nil {
			c.removeJobs(ctx, created.CronJobIDs)
			c.removeLinks(ctx, created.AgentLinkIDs)
			return c.failed(ctx, w, err)
		}

		job, err := c.cron.AddJob(
			ctx,
			jobName(w, i),
			store.CronSchedule{Kind: s.Kind, Expr: s.Expr, EveryMS: s.EveryMS, AtMS: s.AtMS, TZ: s.TZ},
			chainPrompt(s),
			s.Deliver, s.DeliverChan, s.DeliverTo,
			s.AgentID,
			// The workflow's creator, as the ACTOR for the run.
			//
			// This originally passed "" on the reasoning that a workflow belongs to
			// the workspace rather than to whoever armed it. That conflated two
			// different things, and staging showed the cost: the run reached the LLM
			// provider with actor_user="" and was rejected —
			//   400 "Service-token auth requires X-Actor-User-ID and X-Actor-Org-ID"
			// so the schedule fired perfectly and then could not think.
			//
			// Ownership and attribution are separate. The SCHEDULE is tenant-owned —
			// it survives the creator leaving, because the workflow row and its cron
			// job are keyed on the tenant, not on this field. Attribution decides who
			// the call authenticates as and whose usage it counts against, and for a
			// scheduled agent run that is properly the person who set it up.
			creator(w),
		)
		// A nil job with a nil error is a store contract violation, not an
		// impossibility — PGCronStore.AddJob did exactly that when its read-back
		// missed. Treated as a failure rather than dereferenced, because the
		// alternative is a nil panic that takes the gateway down while arming.
		if err == nil && job == nil {
			err = fmt.Errorf("the scheduler accepted the job but returned nothing")
		}
		if err != nil {
			// Partial failure: roll back the jobs already created for THIS apply, so
			// an armed workflow is all-or-nothing. Half a workflow firing is worse
			// than none of it, because the half that runs looks like success.
			c.removeJobs(ctx, created.CronJobIDs)
			c.removeLinks(ctx, created.AgentLinkIDs)
			return c.failed(ctx, w, fmt.Errorf("could not schedule step %d: %w", i+1, err))
		}
		created.CronJobIDs = append(created.CronJobIDs, job.ID)
	}

	slog.Info("workflow.armed", "workflow", w.ID, "tenant", w.TenantID, "jobs", len(created.CronJobIDs))
	return c.record(ctx, w, created, nil)
}

// Retract removes everything a workflow's last compile created, without touching
// the graph. Used before deletion, where the row (and with it the record of what
// to retract) is about to disappear.
func (c *Compiler) Retract(ctx context.Context, w *store.Workflow) {
	if !c.Available() {
		return
	}
	c.retract(ctx, w)
}

func (c *Compiler) retract(ctx context.Context, w *store.Workflow) {
	var prev Compiled
	if len(w.Compiled) > 0 {
		if err := json.Unmarshal(w.Compiled, &prev); err != nil {
			// A corrupt record means we cannot name the jobs to remove. Log loudly:
			// this is the one failure mode that leaves something running that nobody
			// can see the source of.
			slog.Error("workflow.retract.unreadable_compile_record",
				"workflow", w.ID, "compiled", truncate(string(w.Compiled), 200))
			return
		}
	}
	c.removeJobs(ctx, prev.CronJobIDs)
	c.removeLinks(ctx, prev.AgentLinkIDs)
}

func (c *Compiler) removeJobs(ctx context.Context, ids []string) {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := c.cron.RemoveJob(ctx, id); err != nil {
			// Already gone is the common case (a user deleted the job by hand), and
			// it is not worth failing a disarm over. Logged at debug so a genuine
			// pattern is still findable.
			slog.Debug("workflow.retract.remove_job", "job", id, "error", err)
		}
	}
}

// ensureLinks creates the delegation links a chain needs, returning the ids of the
// ones IT created — never ids of links that already existed, which must survive a
// disarm because a user may depend on them elsewhere.
func (c *Compiler) ensureLinks(ctx context.Context, w *store.Workflow, s Step) ([]string, error) {
	if len(s.Handoffs) == 0 {
		return nil, nil
	}
	if c.links == nil {
		return nil, fmt.Errorf("this deployment cannot chain agents")
	}

	// The chain is first → handoff[0] → handoff[1] …, so links are consecutive
	// pairs starting from the step's own agent.
	from := s.AgentID
	var madeIDs []string
	for _, h := range s.Handoffs {
		src, err := uuid.Parse(from)
		if err != nil {
			return madeIDs, fmt.Errorf("agent %q is not a valid id", from)
		}
		dst, err := uuid.Parse(h.AgentID)
		if err != nil {
			return madeIDs, fmt.Errorf("agent %q is not a valid id", h.AgentID)
		}

		// Reuse an existing link rather than adding a duplicate — and do NOT record
		// it, so disarming does not revoke a permission the user set up themselves.
		if ok, err := c.links.CanDelegate(ctx, src, dst); err == nil && ok {
			from = h.AgentID
			continue
		}

		link := &store.AgentLinkData{
			SourceAgentID: src,
			TargetAgentID: dst,
			Direction:     store.LinkDirectionOutbound,
			Description:   fmt.Sprintf("workflow handoff: %s", w.Name),
			MaxConcurrent: 1,
			Status:        "active",
		}
		if w.CreatedBy != nil {
			link.CreatedBy = *w.CreatedBy
		}
		if err := c.links.CreateLink(ctx, link); err != nil {
			return madeIDs, fmt.Errorf("could not allow the handoff to %s: %w", h.AgentID, err)
		}
		madeIDs = append(madeIDs, link.ID.String())
		from = h.AgentID
	}
	return madeIDs, nil
}

func (c *Compiler) removeLinks(ctx context.Context, ids []string) {
	if c.links == nil {
		return
	}
	for _, raw := range ids {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if err := c.links.DeleteLink(ctx, id); err != nil {
			slog.Debug("workflow.retract.remove_link", "link", id, "error", err)
		}
	}
}

// chainPrompt is the instruction the first agent receives.
//
// For a single step it is just the prompt. For a chain it names each subsequent
// agent and what to ask it, because the handoff is MODEL-DRIVEN: the delegate tool
// is available and permitted, but nothing makes the agent use it except being told
// to. Being explicit about the sequence is the difference between a chain that
// happens and one that does not.
func chainPrompt(s Step) string {
	if len(s.Handoffs) == 0 {
		return s.Prompt
	}
	var b strings.Builder
	b.WriteString(s.Prompt)
	b.WriteString("\n\nThen hand the result on, in this order, using the delegate tool:")
	for i, h := range s.Handoffs {
		fmt.Fprintf(&b, "\n%d. delegate to agent %s — %s", i+1, h.AgentID, h.Prompt)
	}
	b.WriteString("\n\nWait for each agent's reply before moving to the next, and treat the LAST reply as the final result.")
	return b.String()
}

func (c *Compiler) record(ctx context.Context, w *store.Workflow, compiled Compiled, compileErr *string) error {
	blob, err := json.Marshal(compiled)
	if err != nil {
		return fmt.Errorf("encode compile record: %w", err)
	}
	w.Compiled = blob
	w.CompileError = compileErr
	return c.workflows.SetCompileResult(ctx, w.TenantID, w.ID, blob, compileErr)
}

// failed records a compile error and returns nil: a failed compile is a normal,
// user-visible state (the message goes on the row and into the UI), not a
// transport error for the caller to translate.
func (c *Compiler) failed(ctx context.Context, w *store.Workflow, cause error) error {
	msg := cause.Error()
	slog.Info("workflow.compile_failed", "workflow", w.ID, "tenant", w.TenantID, "error", msg)
	return c.record(ctx, w, Compiled{}, &msg)
}

// ReconcileAll rebuilds schedules for every armed workflow.
//
// Called on startup because cron jobs and workflows live in separate tables with
// no FK between them: a restore, a manual deletion, or a crash between AddJob and
// SetCompileResult can leave them disagreeing. Re-applying is idempotent — each
// Apply retracts the recorded ids first — so this converges rather than
// duplicating.
func (c *Compiler) ReconcileAll(ctx context.Context) {
	if !c.Available() {
		return
	}
	rows, err := c.workflows.ListEnabled(ctx)
	if err != nil {
		slog.Error("workflow.reconcile.list_failed", "error", err)
		return
	}
	var armed, failed int
	for _, w := range rows {
		if err := c.Apply(ctx, w); err != nil {
			failed++
			slog.Error("workflow.reconcile.apply_failed", "workflow", w.ID, "error", err)
			continue
		}
		if w.CompileError == nil {
			armed++
		}
	}
	if len(rows) > 0 {
		slog.Info("workflow.reconcile.done", "workflows", len(rows), "armed", armed, "failed", failed)
	}
}

// creator returns the user a workflow's runs act as. Empty when unknown, which
// is honest: a workflow created before this field was recorded has no one to
// attribute to, and inventing an actor would be worse than the run failing
// visibly at the provider.
func creator(w *store.Workflow) string {
	if w.CreatedBy == nil {
		return ""
	}
	return strings.TrimSpace(*w.CreatedBy)
}

// jobName labels the cron job so someone reading the cron list can tell where it
// came from and which workflow to edit — an unexplained schedule is the thing
// people are most reluctant to delete.
func jobName(w *store.Workflow, i int) string {
	base := fmt.Sprintf("workflow:%s#%d", w.Name, i+1)
	return truncate(base, 120)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
