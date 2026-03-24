package tools

import "context"

// TaskActionFlags tracks which team_tasks actions a member agent took during a turn.
// The consumer reads these flags post-turn to decide whether to auto-complete,
// skip, or re-dispatch the task.
//
// No mutex needed: a single goroutine writes (agent loop), and the consumer
// reads only after the turn ends (sequential).
type TaskActionFlags struct {
	Completed  bool // team_tasks(action="complete")
	Reviewed   bool // team_tasks(action="review")
	Escalated  bool // team_tasks(action="comment", type="blocker")
	Progressed bool // team_tasks(action="progress")
	Commented  bool // team_tasks(action="comment")
	Claimed    bool // team_tasks(action="claim")
}

// HasAny returns true if the member interacted with the task system at all.
func (f *TaskActionFlags) HasAny() bool {
	return f.Completed || f.Reviewed || f.Escalated || f.Progressed || f.Commented || f.Claimed
}

const ctxTaskActionFlags toolContextKey = "tool_task_action_flags"

// WithTaskActionFlags injects a TaskActionFlags into context.
func WithTaskActionFlags(ctx context.Context, flags *TaskActionFlags) context.Context {
	return context.WithValue(ctx, ctxTaskActionFlags, flags)
}

// TaskActionFlagsFromCtx returns the TaskActionFlags from context, or nil.
func TaskActionFlagsFromCtx(ctx context.Context) *TaskActionFlags {
	v, _ := ctx.Value(ctxTaskActionFlags).(*TaskActionFlags)
	return v
}

// recordTaskAction is a helper that sets a flag if TaskActionFlags exist in context.
func recordTaskAction(ctx context.Context, setter func(*TaskActionFlags)) {
	if flags := TaskActionFlagsFromCtx(ctx); flags != nil {
		setter(flags)
	}
}
