// Package permissions provides role-based access control for gateway methods.
//
// GoClaw uses a 5-layer permission system:
//
//  1. Gateway Auth (token/password, scopes: admin/read/write/approvals/pairing)
//  2. Global Tool Policy (tools.allow[], tools.deny[], tools.profile)
//  3. Per-Agent Policy (agents.list[].tools.allow/deny)
//  4. Per-Channel/Group Policy (channels.*.groups.*.tools.policy)
//  5. Owner-Only Tools (senderIsOwner check)
//
// This package handles layers 1 and 5. Layer 2-4 are handled by internal/tools/policy.go.
package permissions

import (
	"slices"
	"strings"
	"sync"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Role represents a user's permission level.
type Role string

const (
	RoleOwner    Role = "owner"    // Tenant management + full access (superset of admin)
	RoleAdmin    Role = "admin"    // Full access to all methods
	RoleOperator Role = "operator" // Read + write access (no admin operations)
	RoleViewer   Role = "viewer"   // Read-only access

	// RoleNone is a sentinel returned by MethodRole for methods that have no
	// explicit classification. The router treats it as deny-for-everyone so
	// newly-added RPCs are secure-by-default (fail-closed).
	RoleNone Role = ""
)

// Scope represents a specific permission scope.
type Scope string

const (
	ScopeAdmin     Scope = "operator.admin"
	ScopeRead      Scope = "operator.read"
	ScopeWrite     Scope = "operator.write"
	ScopeApprovals Scope = "operator.approvals"
	ScopePairing   Scope = "operator.pairing"
	ScopeProvision Scope = "operator.provision"
)

// AllScopes is the set of all valid API key scopes.
var AllScopes = map[Scope]bool{
	ScopeAdmin:     true,
	ScopeRead:      true,
	ScopeWrite:     true,
	ScopeApprovals: true,
	ScopePairing:   true,
	ScopeProvision: true,
}

// ValidScope reports whether s is a recognised API key scope.
func ValidScope(s string) bool {
	return AllScopes[Scope(s)]
}

// PolicyEngine evaluates user permissions for gateway method access.
type PolicyEngine struct {
	ownerIDs map[string]bool // sender IDs that are considered "owner"
	mu       sync.RWMutex
}

// NewPolicyEngine creates a new permission policy engine.
func NewPolicyEngine(ownerIDs []string) *PolicyEngine {
	owners := make(map[string]bool, len(ownerIDs))
	for _, id := range ownerIDs {
		owners[id] = true
	}
	return &PolicyEngine{
		ownerIDs: owners,
	}
}

// IsOwner checks if a sender ID is an owner.
// When no owner IDs are configured, "system" is treated as owner (fail-closed default).
func (pe *PolicyEngine) IsOwner(senderID string) bool {
	if senderID == "" {
		return false
	}
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if len(pe.ownerIDs) == 0 {
		return senderID == "system"
	}
	return pe.ownerIDs[senderID]
}

// CanAccess checks if a role has access to a gateway RPC method.
// Unclassified methods (MethodRole == RoleNone) are denied for every role
// including owner — callers must surface an explicit PERMISSION_DENIED/
// UNAUTHORIZED and never silently permit.
func (pe *PolicyEngine) CanAccess(role Role, method string) bool {
	requiredRole := MethodRole(method)
	if requiredRole == RoleNone {
		return false
	}
	return roleLevel(role) >= roleLevel(requiredRole)
}

// CanAccessWithScopes checks if the given scopes permit access to a method.
func (pe *PolicyEngine) CanAccessWithScopes(scopes []Scope, method string) bool {
	required := MethodScopes(method)
	if len(required) == 0 {
		return true // no scope restriction
	}

	scopeSet := make(map[Scope]bool, len(scopes))
	for _, s := range scopes {
		scopeSet[s] = true
	}

	for _, r := range required {
		if scopeSet[r] {
			return true
		}
	}
	return false
}

// RoleFromScopes determines the effective role from a set of scopes.
func RoleFromScopes(scopes []Scope) Role {
	if slices.Contains(scopes, ScopeAdmin) {
		return RoleAdmin
	}
	if slices.Contains(scopes, ScopeWrite) ||
		slices.Contains(scopes, ScopeApprovals) ||
		slices.Contains(scopes, ScopePairing) {
		return RoleOperator
	}
	if slices.Contains(scopes, ScopeRead) {
		return RoleViewer
	}
	return RoleViewer
}

// MethodRole returns the minimum role required for a given RPC method.
//
// Policy is fail-closed (default-deny): methods absent from every allowlist
// return RoleNone. The gateway dispatcher must reject RoleNone with an
// UNAUTHORIZED / PERMISSION_DENIED error rather than granting viewer access.
// This is the fix for issue #866 where default-permit let unauthenticated
// clients invoke mutation/exfiltration RPCs (heartbeat.*, logs.tail, etc.).
func MethodRole(method string) Role {
	// System methods that bypass auth entirely (pre-auth handshake only).
	if isPublicMethod(method) {
		return RoleViewer
	}

	// Admin-only methods
	if isAdminMethod(method) {
		return RoleAdmin
	}

	// Write methods (require operator or above)
	if isWriteMethod(method) {
		return RoleOperator
	}

	// Read-only methods (viewer and above)
	if isReadMethod(method) {
		return RoleViewer
	}

	// Fail-closed: unknown / unclassified method → deny.
	return RoleNone
}

// isPublicMethod lists methods every authenticated (and some pre-auth) client
// is allowed to call. Kept tiny on purpose — do not expand without review.
func isPublicMethod(method string) bool {
	switch method {
	case protocol.MethodConnect,
		protocol.MethodHealth,
		protocol.MethodStatus,
		protocol.MethodBrowserPairingStatus:
		return true
	}
	return false
}

// MethodScopes returns the scopes required for a method.
func MethodScopes(method string) []Scope {
	if isAdminMethod(method) {
		return []Scope{ScopeAdmin}
	}
	if strings.HasPrefix(method, "approvals.") {
		return []Scope{ScopeApprovals, ScopeAdmin}
	}
	if strings.HasPrefix(method, "pairing.") || strings.HasPrefix(method, "device.pair") {
		return []Scope{ScopePairing, ScopeAdmin}
	}
	if isWriteMethod(method) {
		return []Scope{ScopeWrite, ScopeAdmin}
	}
	return []Scope{ScopeRead, ScopeWrite, ScopeAdmin}
}

func isAdminMethod(method string) bool {
	adminMethods := []string{
		// Config — admin/owner only. Additional master-scope/owner guards live
		// in the handler middleware, but classify here for defense-in-depth.
		protocol.MethodConfigGet,
		protocol.MethodConfigApply,
		protocol.MethodConfigPatch,
		protocol.MethodConfigSchema,
		protocol.MethodConfigDefaults,
		protocol.MethodConfigPermissionsList,
		protocol.MethodConfigPermissionsGrant,
		protocol.MethodConfigPermissionsRevoke,

		// Agents — create/update/delete and link mutations.
		protocol.MethodAgentsCreate,
		protocol.MethodAgentsUpdate,
		protocol.MethodAgentsDelete,
		protocol.MethodAgentsLinksCreate,
		protocol.MethodAgentsLinksUpdate,
		protocol.MethodAgentsLinksDelete,

		// Channels.
		protocol.MethodChannelsToggle,
		protocol.MethodChannelInstancesCreate,
		protocol.MethodChannelInstancesUpdate,
		protocol.MethodChannelInstancesDelete,

		// Pairing management (approve/revoke/list/deny require admin).
		protocol.MethodPairingApprove,
		protocol.MethodPairingDeny,
		protocol.MethodPairingList,
		protocol.MethodPairingRevoke,

		// Teams — create/delete/update/member management.
		protocol.MethodTeamsCreate,
		protocol.MethodTeamsDelete,
		protocol.MethodTeamsUpdate,
		protocol.MethodTeamsMembersAdd,
		protocol.MethodTeamsMembersRemove,
		protocol.MethodTeamsTaskDelete,
		protocol.MethodTeamsTaskDeleteBulk,

		// Tenants — write paths.
		"tenants.create",
		"tenants.update",
		"tenants.users.add",
		"tenants.users.remove",

		// API keys expose secret material — gate list + mutations as admin.
		protocol.MethodAPIKeysList,
		protocol.MethodAPIKeysCreate,
		protocol.MethodAPIKeysRevoke,

		// Skills (can rewrite agent behavior).
		protocol.MethodSkillsUpdate,

		// Heartbeat — any write/test path (closes CVE #866 step 2 + step 4).
		protocol.MethodHeartbeatSet,
		protocol.MethodHeartbeatToggle,
		protocol.MethodHeartbeatTest,
		protocol.MethodHeartbeatChecklistSet,

		// Live server logs — data exfiltration risk (closes CVE #866 step 3).
		protocol.MethodLogsTail,

		// Hooks mutations (the handler middleware also enforces this).
		protocol.MethodHooksCreate,
		protocol.MethodHooksUpdate,
		protocol.MethodHooksDelete,
		protocol.MethodHooksToggle,

		// Voice catalogue refresh touches provider credentials.
		protocol.MethodVoicesRefresh,

		// TTS config mutations touch provider credentials / global state.
		protocol.MethodTTSEnable,
		protocol.MethodTTSDisable,
		protocol.MethodTTSSetProvider,
	}
	return slices.Contains(adminMethods, method)
}

func isWriteMethod(method string) bool {
	writeExact := []string{
		protocol.MethodChatSend,
		protocol.MethodChatAbort,
		protocol.MethodChatInject,
		protocol.MethodSessionsDelete,
		protocol.MethodSessionsReset,
		protocol.MethodSessionsPatch,
		protocol.MethodSessionsCompact,
		protocol.MethodCronCreate,
		protocol.MethodCronUpdate,
		protocol.MethodCronDelete,
		protocol.MethodCronToggle,
		protocol.MethodCronRun,
		protocol.MethodSend,
		protocol.MethodAgentsFileSet,
		protocol.MethodTeamsTaskApprove,
		protocol.MethodTeamsTaskReject,
		protocol.MethodTeamsTaskComment,
		protocol.MethodTeamsTaskCreate,
		protocol.MethodTeamsTaskAssign,
		protocol.MethodTeamsWorkspaceDelete,
		protocol.MethodHooksTest,
		protocol.MethodPairingRequest,
		protocol.MethodApprovalsApprove,
		protocol.MethodApprovalsDeny,

		// TTS synthesis — invokes provider API (quota/credentials).
		protocol.MethodTTSConvert,

		// Browser automation — performs side-effecting actions.
		protocol.MethodBrowserAct,

		// Channel pairing starts (QR scan flows).
		protocol.MethodZaloPersonalQRStart,
		protocol.MethodWhatsAppQRStart,
	}
	return slices.Contains(writeExact, method)
}

// isReadMethod is the explicit viewer-allowlist for read-only RPCs. Keeping
// this as a closed list (rather than "everything else") is what turns the
// policy into fail-closed. New read RPCs must be added here explicitly.
func isReadMethod(method string) bool {
	readMethods := []string{
		// Agent identity / wait
		protocol.MethodAgent,
		protocol.MethodAgentWait,
		protocol.MethodAgentIdentityGet,

		// Chat read
		protocol.MethodChatHistory,
		protocol.MethodChatSessionStatus,

		// Agents read
		protocol.MethodAgentsList,
		protocol.MethodAgentsFileList,
		protocol.MethodAgentsFileGet,
		protocol.MethodAgentsLinksList,

		// Sessions read
		protocol.MethodSessionsList,
		protocol.MethodSessionsPreview,

		// Skills read
		protocol.MethodSkillsList,
		protocol.MethodSkillsGet,

		// Cron read
		protocol.MethodCronList,
		protocol.MethodCronStatus,
		protocol.MethodCronRuns,

		// Channels read
		protocol.MethodChannelsList,
		protocol.MethodChannelsStatus,
		protocol.MethodChannelInstancesList,
		protocol.MethodChannelInstancesGet,

		// Usage / quota
		protocol.MethodUsageGet,
		protocol.MethodUsageSummary,
		protocol.MethodQuotaUsage,

		// Heartbeat read
		protocol.MethodHeartbeatGet,
		protocol.MethodHeartbeatLogs,
		protocol.MethodHeartbeatChecklistGet,
		protocol.MethodHeartbeatTargets,

		// Voices
		protocol.MethodVoicesList,

		// Tenants read
		"tenants.list",
		"tenants.get",
		"tenants.users.list",
		"tenants.mine",

		// Teams read
		protocol.MethodTeamsList,
		protocol.MethodTeamsGet,
		protocol.MethodTeamsTaskList,
		protocol.MethodTeamsTaskGet,
		protocol.MethodTeamsTaskGetLight,
		protocol.MethodTeamsTaskComments,
		protocol.MethodTeamsTaskEvents,
		protocol.MethodTeamsTaskActiveBySession,
		protocol.MethodTeamsWorkspaceList,
		protocol.MethodTeamsWorkspaceRead,
		protocol.MethodTeamsEventsList,
		protocol.MethodTeamsKnownUsers,
		protocol.MethodTeamsScopes,

		// Hooks read
		protocol.MethodHooksList,
		protocol.MethodHooksHistory,

		// Approvals read-only listing
		protocol.MethodApprovalsList,

		// TTS read-only (status/providers listing)
		protocol.MethodTTSStatus,
		protocol.MethodTTSProviders,

		// Browser observation (no side effects)
		protocol.MethodBrowserSnapshot,
		protocol.MethodBrowserScreenshot,

		// Zalo personal contacts listing
		protocol.MethodZaloPersonalContacts,
	}
	return slices.Contains(readMethods, method)
}

// HasMinRole checks if the given role meets the minimum required level.
func HasMinRole(role, required Role) bool {
	return roleLevel(role) >= roleLevel(required)
}

func roleLevel(r Role) int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}
