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

// FullTeamPolicy allows all 18 team task actions (standard/PG edition).
type FullTeamPolicy struct{}

var fullActions = []string{
	"list", "get", "create", "claim", "complete", "cancel",
	"approve", "reject", "search", "review", "comment",
	"progress", "attach", "update", "ask_user", "clear_ask_user", "retry",
}

func (FullTeamPolicy) IsAllowed(string) bool       { return true }
func (FullTeamPolicy) AllowedActions() []string     { return fullActions }
func (FullTeamPolicy) MemberGuidance() string {
	return "Use comment(type='blocker') to escalate blockers to the leader. " +
		"Use review to submit work for approval. " +
		"Use progress to report incremental status updates."
}

// LiteTeamPolicy allows core lifecycle actions only (desktop/lite edition).
// Blocked: comment, review, approve, reject, attach, ask_user, clear_ask_user.
type LiteTeamPolicy struct{}

var liteActions = []string{
	"list", "get", "create", "claim", "complete", "cancel",
	"progress", "search", "update", "retry",
}

var liteBlocked = map[string]bool{
	"comment": true, "review": true, "approve": true, "reject": true,
	"attach": true, "ask_user": true, "clear_ask_user": true,
}

func (LiteTeamPolicy) IsAllowed(action string) bool { return !liteBlocked[action] }
func (LiteTeamPolicy) AllowedActions() []string      { return liteActions }
func (LiteTeamPolicy) MemberGuidance() string {
	return "Use progress to update status. Use complete when finished."
}
