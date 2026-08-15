//go:build integration

package invariants

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// INVARIANT: Admin-only operations MUST reject Operator role.
func TestPermission_AdminOnlyRejectsOperator(t *testing.T) {
	pe := permissions.NewPolicyEngine(nil)

	adminMethods := []string{
		protocol.MethodConfigApply,
		protocol.MethodConfigPatch,
		protocol.MethodAgentsCreate,
		protocol.MethodAgentsUpdate,
		protocol.MethodAgentsDelete,
		protocol.MethodTeamsCreate,
		protocol.MethodTeamsDelete,
		protocol.MethodAPIKeysCreate,
		protocol.MethodAPIKeysRevoke,
	}

	for _, method := range adminMethods {
		t.Run(method, func(t *testing.T) {
			// INVARIANT: Operator MUST NOT access admin methods
			if pe.CanAccess(permissions.RoleOperator, method) {
				t.Errorf("INVARIANT VIOLATION: Operator can access admin method %s", method)
			}

			// INVARIANT: Viewer MUST NOT access admin methods
			if pe.CanAccess(permissions.RoleViewer, method) {
				t.Errorf("INVARIANT VIOLATION: Viewer can access admin method %s", method)
			}

			// Admin and Owner should have access
			if !pe.CanAccess(permissions.RoleAdmin, method) {
				t.Errorf("Admin should access %s", method)
			}
			if !pe.CanAccess(permissions.RoleOwner, method) {
				t.Errorf("Owner should access %s", method)
			}
		})
	}
}

// INVARIANT: Write operations MUST reject Viewer role.
func TestPermission_WriteRejectsViewer(t *testing.T) {
	pe := permissions.NewPolicyEngine(nil)

	writeMethods := []string{
		protocol.MethodChatSend,
		protocol.MethodChatAbort,
		protocol.MethodSessionsDelete,
		protocol.MethodSessionsReset,
		protocol.MethodCronCreate,
		protocol.MethodCronUpdate,
		protocol.MethodCronDelete,
	}

	for _, method := range writeMethods {
		t.Run(method, func(t *testing.T) {
			// INVARIANT: Viewer MUST NOT access write methods
			if pe.CanAccess(permissions.RoleViewer, method) {
				t.Errorf("INVARIANT VIOLATION: Viewer can access write method %s", method)
			}

			// Operator, Admin, and Owner should have access
			if !pe.CanAccess(permissions.RoleOperator, method) {
				t.Errorf("Operator should access %s", method)
			}
		})
	}
}

// INVARIANT: Owner MUST have access to all methods (superset of Admin).
func TestPermission_OwnerSupersetOfAdmin(t *testing.T) {
	pe := permissions.NewPolicyEngine(nil)

	allMethods := []string{
		// Admin methods
		protocol.MethodConfigApply,
		protocol.MethodAgentsCreate,
		protocol.MethodTeamsCreate,
		// Write methods
		protocol.MethodChatSend,
		protocol.MethodSessionsDelete,
		// Read methods
		protocol.MethodSessionsList,
		protocol.MethodAgentsList,
	}

	for _, method := range allMethods {
		t.Run(method, func(t *testing.T) {
			adminCan := pe.CanAccess(permissions.RoleAdmin, method)
			ownerCan := pe.CanAccess(permissions.RoleOwner, method)

			// INVARIANT: If Admin can access, Owner MUST be able to access
			if adminCan && !ownerCan {
				t.Errorf("INVARIANT VIOLATION: Admin can access %s but Owner cannot", method)
			}

			// INVARIANT: Owner MUST always have access
			if !ownerCan {
				t.Errorf("INVARIANT VIOLATION: Owner cannot access %s", method)
			}
		})
	}
}

// INVARIANT: Role hierarchy MUST be strictly ordered: Owner > Admin > Operator > Viewer.
func TestPermission_RoleHierarchy(t *testing.T) {
	tests := []struct {
		name     string
		higher   permissions.Role
		lower    permissions.Role
		required permissions.Role
	}{
		{"owner_beats_admin", permissions.RoleOwner, permissions.RoleAdmin, permissions.RoleAdmin},
		{"admin_beats_operator", permissions.RoleAdmin, permissions.RoleOperator, permissions.RoleAdmin},
		{"operator_beats_viewer", permissions.RoleOperator, permissions.RoleViewer, permissions.RoleOperator},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			higherMeets := permissions.HasMinRole(tt.higher, tt.required)
			lowerMeets := permissions.HasMinRole(tt.lower, tt.required)

			// INVARIANT: Higher role MUST meet requirement that lower cannot
			if lowerMeets && !higherMeets {
				t.Errorf("INVARIANT VIOLATION: %s cannot meet %s but %s can",
					tt.higher, tt.required, tt.lower)
			}
		})
	}
}

// INVARIANT: API key scopes MUST map correctly to roles.
func TestPermission_ScopesToRoleMapping(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []permissions.Scope
		expected permissions.Role
	}{
		{"admin_scope_is_admin", []permissions.Scope{permissions.ScopeAdmin}, permissions.RoleAdmin},
		{"write_scope_is_operator", []permissions.Scope{permissions.ScopeWrite}, permissions.RoleOperator},
		{"read_scope_is_viewer", []permissions.Scope{permissions.ScopeRead}, permissions.RoleViewer},
		{"empty_scope_is_viewer", []permissions.Scope{}, permissions.RoleViewer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := permissions.RoleFromScopes(tt.scopes)
			if got != tt.expected {
				t.Errorf("INVARIANT VIOLATION: scopes %v should map to %s, got %s",
					tt.scopes, tt.expected, got)
			}
		})
	}
}

// INVARIANT: Owner IDs MUST be consistently recognized.
func TestPermission_OwnerRecognition(t *testing.T) {
	owners := []string{"owner-alice", "owner-bob"}
	pe := permissions.NewPolicyEngine(owners)

	// INVARIANT: Listed owners MUST be recognized
	for _, owner := range owners {
		if !pe.IsOwner(owner) {
			t.Errorf("INVARIANT VIOLATION: %s should be recognized as owner", owner)
		}
	}

	// INVARIANT: Non-owners MUST NOT be recognized as owners
	nonOwners := []string{"charlie", "admin", "", "owner"}
	for _, nonOwner := range nonOwners {
		if pe.IsOwner(nonOwner) {
			t.Errorf("INVARIANT VIOLATION: %s should NOT be recognized as owner", nonOwner)
		}
	}
}

// INVARIANT: Config permission store MUST enforce tenant scoping.
func TestPermission_ConfigPermissionTenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	cps := pg.NewPGConfigPermissionStore(db)

	// Grant permission in tenant A using colon-delimited scope (matchWildcard requires :* format)
	err := cps.Grant(ctxA, &store.ConfigPermission{
		AgentID:    agentA,
		Scope:      "group:*",
		ConfigType: "file_writer",
		Permission: "allow",
		UserID:     "user-a",
	})
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_config_permissions WHERE agent_id = $1", agentA)
	})

	// Verify tenant A has permission (scope must match pattern: group:* matches group:telegram)
	allowed, err := cps.CheckPermission(ctxA, agentA, "group:telegram", "file_writer", "user-a")
	if err != nil {
		t.Fatalf("CheckPermission A: %v", err)
	}
	if !allowed {
		t.Error("tenant A should have permission on own agent")
	}

	// INVARIANT: Tenant B MUST NOT see tenant A's permissions
	allowedB, err := cps.CheckPermission(ctxB, agentA, "group:telegram", "file_writer", "user-a")
	if err != nil {
		t.Fatalf("CheckPermission B: %v", err)
	}
	if allowedB {
		t.Errorf("INVARIANT VIOLATION: tenant B sees tenant A's permission")
	}
}

// INVARIANT: Scope-based access MUST require correct scope for method.
func TestPermission_ScopeBasedAccess(t *testing.T) {
	pe := permissions.NewPolicyEngine(nil)

	tests := []struct {
		name        string
		scopes      []permissions.Scope
		method      string
		shouldAllow bool
	}{
		// Admin methods require admin scope
		{"read_cannot_create_agent", []permissions.Scope{permissions.ScopeRead}, protocol.MethodAgentsCreate, false},
		{"write_cannot_create_agent", []permissions.Scope{permissions.ScopeWrite}, protocol.MethodAgentsCreate, false},
		{"admin_can_create_agent", []permissions.Scope{permissions.ScopeAdmin}, protocol.MethodAgentsCreate, true},

		// Write methods require write or admin scope
		{"read_cannot_send_chat", []permissions.Scope{permissions.ScopeRead}, protocol.MethodChatSend, false},
		{"write_can_send_chat", []permissions.Scope{permissions.ScopeWrite}, protocol.MethodChatSend, true},
		{"admin_can_send_chat", []permissions.Scope{permissions.ScopeAdmin}, protocol.MethodChatSend, true},

		// Approval methods require approvals or admin scope
		{"read_cannot_list_approvals", []permissions.Scope{permissions.ScopeRead}, "approvals.list", false},
		{"approvals_can_list_approvals", []permissions.Scope{permissions.ScopeApprovals}, "approvals.list", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pe.CanAccessWithScopes(tt.scopes, tt.method)
			if got != tt.shouldAllow {
				if tt.shouldAllow {
					t.Errorf("INVARIANT VIOLATION: scopes %v should allow %s", tt.scopes, tt.method)
				} else {
					t.Errorf("INVARIANT VIOLATION: scopes %v should NOT allow %s", tt.scopes, tt.method)
				}
			}
		})
	}
}
