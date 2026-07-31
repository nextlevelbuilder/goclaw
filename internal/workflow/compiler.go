package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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
}

// Compiler arms and disarms workflows by creating and removing cron jobs.
type Compiler struct {
	workflows store.WorkflowStore
	cron      store.CronStore
}

func NewCompiler(workflows store.WorkflowStore, cron store.CronStore) *Compiler {
	return &Compiler{workflows: workflows, cron: cron}
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
		job, err := c.cron.AddJob(
			ctx,
			jobName(w, i),
			store.CronSchedule{Kind: s.Kind, Expr: s.Expr, EveryMS: s.EveryMS, AtMS: s.AtMS, TZ: s.TZ},
			s.Prompt,
			s.Deliver, s.DeliverChan, s.DeliverTo,
			s.AgentID,
			// No user id: a workflow's runs belong to the workspace, not to whoever
			// last pressed Arm. Attributing them to that person would make their
			// departure silently orphan the schedule.
			"",
		)
		if err != nil {
			// Partial failure: roll back the jobs already created for THIS apply, so
			// an armed workflow is all-or-nothing. Half a workflow firing is worse
			// than none of it, because the half that runs looks like success.
			created.CronJobIDs = append(created.CronJobIDs, "")
			c.removeJobs(ctx, created.CronJobIDs)
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
