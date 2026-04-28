package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// runContextKey is the context key for RunContext.
type runContextKey struct{}

// RunContext consolidates all agent-loop-injected context values into a single
// typed struct. This replaces 27 individual context.WithValue calls with one
// WithRunContext call, improving readability and making it trivial to add new
// scope fields (e.g. ProjectID).
//
// Consumers can read via RunContextFromCtx() or continue using existing
// accessor functions (which fall back to individual keys when RunContext is absent).
type RunContext struct {
	// Identity
	AgentID          uuid.UUID
	AgentKey         string
	TenantID         uuid.UUID
	UserID           string
	CredentialUserID string // resolved tenant user for credential lookups (empty = use UserID)
	AgentType        string
	SenderID         string

	// Flags
	SelfEvolve          bool
	SharedMemory        bool
	SharedKG            bool
	SharedSessions      bool
	RestrictToWorkspace bool

	// Tool configuration
	BuiltinToolSettings map[string][]byte
	ChannelType         string
	SubagentsCfg        *config.SubagentsConfig
	ParentModel         string
	ParentProvider      string
	MemoryCfg           *config.MemoryConfig
	SandboxCfg          *sandbox.Config
	ShellDenyGroups     map[string]bool

	// Workspace
	Workspace          string
	TeamWorkspace      string
	TeamID             string
	WorkspaceChannel   string
	WorkspaceChatID    string
	TeamIsolated       bool   // true when team.workspace_scope != "shared" — drives chat_id filtering in vault search
	TeamTaskID         string
	DelegationID       string   // delegation identifier for vault auto-linking (empty when not in delegation)
	LeaderAgentID      string   // leader's agent UUID for member memory read fallback
	AgentToolKey       string   // tool-level agent key for registry routing
	TenantAllowedPaths []string // tenant-specific allowed paths beyond workspace (from system_configs)
}

// WithRunContext stores a RunContext on the context.
func WithRunContext(ctx context.Context, rc *RunContext) context.Context {
	return context.WithValue(ctx, runContextKey{}, rc)
}

// RunContextFromCtx extracts RunContext from context. Returns nil if not set.
func RunContextFromCtx(ctx context.Context) *RunContext {
	rc, _ := ctx.Value(runContextKey{}).(*RunContext)
	return rc
}
