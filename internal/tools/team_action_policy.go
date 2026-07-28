package tools

// TeamActionPolicy controls which team_tasks actions are available.
// Injected into TeamTasksTool at construction — no scattered if/else.
type TeamActionPolicy interface {
	// IsAllowed returns true if the action is permitted in this edition.
	IsAllowed(action string) bool
	// AllowedActions returns the list of permitted action names (for Schema enum).
	AllowedActions() []string
	// MemberGuidance returns system-prompt text for team members.
	MemberGuidance() string
}

// actionAllowed reports membership in a policy's whitelist. Both editions use
// membership rather than a blacklist fallthrough so an action absent from the
// advertised Schema enum (AllowedActions) can never be silently accepted — the
// tool surface and the authorization surface stay in exact agreement.
func actionAllowed(list []string, action string) bool {
	for _, a := range list {
		if a == action {
			return true
		}
	}
	return false
}

// FullTeamPolicy allows all team task actions (standard/PG edition), including
// the five coordinator workflow-recovery actions that a team lead can invoke on a
// blocked/needs-revision workflow without mechanically failing it: retry_blocked,
// request_revision, apply_replan, cancel_workflow, fail_workflow. retry_expansion
// and retry_delivery are deliberately NOT here — they are admin-RPC-only
// (dashboard) operator actions with no coordinator recovery-tool surface.
type FullTeamPolicy struct{}

var fullActions = []string{
	"list", "get", "create", "claim", "complete", "cancel",
	"create_workflow", "get_workflow",
	"approve", "reject", "search", "review", "comment",
	"progress", "attach", "update", "ask_user", "clear_ask_user", "retry",
	"retry_blocked", "request_revision", "apply_replan",
	"cancel_workflow", "fail_workflow",
}

func (FullTeamPolicy) IsAllowed(action string) bool { return actionAllowed(fullActions, action) }
func (FullTeamPolicy) AllowedActions() []string     { return append([]string(nil), fullActions...) }
func (FullTeamPolicy) MemberGuidance() string {
	return "Use comment(type='blocker') to escalate blockers to the leader. " +
		"Use review to submit work for approval. " +
		"Use progress to report incremental status updates."
}

// LiteTeamPolicy allows core lifecycle actions plus the coordinator workflow-
// recovery actions (desktop/lite edition). Blocked: comment, review, approve,
// reject, attach, ask_user, clear_ask_user. The five coordinator recovery actions
// are exposed for cross-edition parity — a Lite team lead resolves a blocked
// workflow with the same recovery vocabulary as Full. retry_expansion and
// retry_delivery remain admin-RPC-only in both editions.
type LiteTeamPolicy struct{}

var liteActions = []string{
	"list", "get", "create", "claim", "complete", "cancel",
	"create_workflow", "get_workflow",
	"progress", "search", "update", "retry",
	"retry_blocked", "request_revision", "apply_replan",
	"cancel_workflow", "fail_workflow",
}

func (LiteTeamPolicy) IsAllowed(action string) bool { return actionAllowed(liteActions, action) }
func (LiteTeamPolicy) AllowedActions() []string     { return append([]string(nil), liteActions...) }
func (LiteTeamPolicy) MemberGuidance() string {
	return "Use progress to update status. Use complete when finished."
}
