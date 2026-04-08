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
func (pe *PolicyEngine) IsOwner(senderID string) bool {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.ownerIDs[senderID]
}

// CanAccess checks if a role has access to a gateway RPC method.
func (pe *PolicyEngine) CanAccess(role Role, method string) bool {
	requiredRole := MethodRole(method)
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
func MethodRole(method string) Role {
	// Admin-only methods
	if isAdminMethod(method) {
		return RoleAdmin
	}

	// Write methods (require operator or above)
	if isWriteMethod(method) {
		return RoleOperator
	}

	// Everything else is read-only (viewer can access)
	return RoleViewer
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
		protocol.MethodNodeInvokeResult,
		protocol.MethodNodeEvent,
		protocol.MethodNodeList,
		protocol.MethodNodeExec,
		protocol.MethodConfigApply,
		protocol.MethodConfigPatch,
		protocol.MethodAgentsCreate,
		protocol.MethodAgentsUpdate,
		protocol.MethodAgentsDelete,
		protocol.MethodAgentsLinksList,
		protocol.MethodAgentsLinksCreate,
		protocol.MethodAgentsLinksUpdate,
		protocol.MethodAgentsLinksDelete,
		protocol.MethodChannelsToggle,
		protocol.MethodPairingApprove,
		protocol.MethodPairingRevoke,
		protocol.MethodTeamsList,
		protocol.MethodTeamsCreate,
		protocol.MethodTeamsGet,
		protocol.MethodTeamsDelete,
		protocol.MethodTeamsTaskList,
		protocol.MethodTeamsTaskGet,
		protocol.MethodTeamsTaskComments,
		protocol.MethodTeamsTaskEvents,
		protocol.MethodAPIKeysList,
		protocol.MethodAPIKeysCreate,
		protocol.MethodAPIKeysRevoke,
		protocol.MethodSkillsUpdate,
	}
	return slices.Contains(adminMethods, method)
}

func isWriteMethod(method string) bool {
	writePrefixes := []string{
		protocol.MethodChatSend,
		protocol.MethodChatAbort,
		protocol.MethodSessionsDelete,
		protocol.MethodSessionsReset,
		protocol.MethodSessionsPatch,
		protocol.MethodCronCreate,
		protocol.MethodCronUpdate,
		protocol.MethodCronDelete,
		protocol.MethodCronToggle,
		"pairing.",
		"device.pair.",
		"approvals.",
		"exec.approval.",
		protocol.MethodSend,
		protocol.MethodTeamsTaskApprove,
		protocol.MethodTeamsTaskReject,
		protocol.MethodTeamsTaskComment,
		protocol.MethodTeamsTaskCreate,
		protocol.MethodTeamsTaskAssign,
	}
	for _, prefix := range writePrefixes {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
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
