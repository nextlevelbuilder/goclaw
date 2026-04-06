// Package workspace provides unified workspace resolution for agent runs.
// Replaces dual ctxWorkspace + ctxTeamWorkspace with a single immutable context.
//
// V3 design: Phase 1B — foundation interface.
package workspace

import "context"

// Scope defines workspace access boundary.
type Scope string

const (
	ScopePersonal Scope = "personal"  // single user, isolated
	ScopeTeam     Scope = "team"      // team context, shared or isolated
	ScopeDelegate Scope = "delegate"  // delegated task, scoped access
)

// WorkspaceContext is resolved ONCE at run start, immutable for the entire run.
// Eliminates dual ctxWorkspace + ctxTeamWorkspace confusion.
type WorkspaceContext struct {
	// ActivePath is THE path for all file operations (read/write/list/exec).
	ActivePath string

	// Scope describes the access boundary type.
	Scope Scope

	// ReadOnlyPaths are additional paths the agent can read but NOT write.
	ReadOnlyPaths []string

	// SharedPath is the shared delegate area (read/write by both delegator + delegatee).
	// nil when not in delegation context.
	SharedPath *string

	// TeamPath is the team workspace root (nil if not in team context).
	TeamPath *string

	// MemoryScope determines memory isolation.
	// Defaults to workspace scope. "shared" = all users in agent see same memory.
	MemoryScope string

	// KGScope determines knowledge graph isolation.
	KGScope string

	// OwnerID identifies who owns this workspace context (user ID or chat ID).
	OwnerID string

	// EnforcementLabel is injected into system prompt verbatim.
	EnforcementLabel string
}

// Resolver produces a WorkspaceContext from request parameters.
// Called once at ContextStage. Result is immutable.
type Resolver interface {
	Resolve(ctx context.Context, params ResolveParams) (*WorkspaceContext, error)
}

// ResolveParams captures all inputs needed to determine workspace.
type ResolveParams struct {
	AgentID    string
	AgentType  string // "open" | "predefined"
	UserID     string
	ChatID     string
	TenantID   string
	TenantSlug string // human-readable tenant name for path composition
	PeerKind   string // "direct" | "group"
	TeamID     *string
	TeamConfig *TeamWorkspaceConfig
	DelegateCtx *DelegateContext
	BaseDir    string
}

// TeamWorkspaceConfig maps to team.settings JSON.
// WorkspaceScope uses "shared"/"isolated" string to match existing DB schema.
type TeamWorkspaceConfig struct {
	WorkspaceScope string `json:"workspace_scope"`
	WorkspacePath  string `json:"workspace_path,omitempty"`
}

// IsShared returns true when workspace_scope is "shared".
func (c *TeamWorkspaceConfig) IsShared() bool {
	return c != nil && c.WorkspaceScope == "shared"
}

// DelegateContext carries delegation-specific workspace overrides.
type DelegateContext struct {
	LinkID      string
	SharedPath  string
	ExportPaths []string // read-only exports from delegator
}

// DefaultEnforcementLabel returns a human-readable workspace description
// for system prompt injection based on scope and sharing mode.
func DefaultEnforcementLabel(scope Scope, shared bool) string {
	switch scope {
	case ScopeDelegate:
		return "You are working on a delegated task. Only access files in your designated workspace."
	case ScopeTeam:
		if shared {
			return "You are working in a shared team workspace. Other members can see your files."
		}
		return "You are working in an isolated team workspace."
	default:
		return "You are working in the user's personal workspace."
	}
}

// context key for WorkspaceContext propagation.
type ctxKeyWorkspace struct{}

// FromContext extracts WorkspaceContext from context.
func FromContext(ctx context.Context) *WorkspaceContext {
	wc, _ := ctx.Value(ctxKeyWorkspace{}).(*WorkspaceContext)
	return wc
}

// WithContext stores WorkspaceContext in context.
func WithContext(ctx context.Context, wc *WorkspaceContext) context.Context {
	return context.WithValue(ctx, ctxKeyWorkspace{}, wc)
}
