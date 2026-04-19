package permissions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- Role hierarchy ---

func TestRoleLevel_Ordering(t *testing.T) {
	// Owner > Admin > Operator > Viewer > unknown
	levels := []struct {
		role  Role
		level int
	}{
		{RoleOwner, 4},
		{RoleAdmin, 3},
		{RoleOperator, 2},
		{RoleViewer, 1},
		{Role("unknown"), 0},
		{Role(""), 0},
	}
	for _, tt := range levels {
		t.Run(string(tt.role), func(t *testing.T) {
			got := roleLevel(tt.role)
			if got != tt.level {
				t.Fatalf("roleLevel(%q) = %d, want %d", tt.role, got, tt.level)
			}
		})
	}
}

func TestHasMinRole(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		required Role
		want     bool
	}{
		{"owner_meets_admin", RoleOwner, RoleAdmin, true},
		{"admin_meets_admin", RoleAdmin, RoleAdmin, true},
		{"operator_fails_admin", RoleOperator, RoleAdmin, false},
		{"viewer_fails_operator", RoleViewer, RoleOperator, false},
		{"operator_meets_viewer", RoleOperator, RoleViewer, true},
		{"admin_meets_viewer", RoleAdmin, RoleViewer, true},
		{"viewer_meets_viewer", RoleViewer, RoleViewer, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasMinRole(tt.role, tt.required)
			if got != tt.want {
				t.Fatalf("HasMinRole(%q, %q) = %v, want %v", tt.role, tt.required, got, tt.want)
			}
		})
	}
}

// --- RoleFromScopes ---

func TestRoleFromScopes(t *testing.T) {
	tests := []struct {
		name   string
		scopes []Scope
		want   Role
	}{
		{"admin_scope", []Scope{ScopeAdmin}, RoleAdmin},
		{"admin_overrides_read", []Scope{ScopeRead, ScopeAdmin}, RoleAdmin},
		{"write_is_operator", []Scope{ScopeWrite}, RoleOperator},
		{"approvals_is_operator", []Scope{ScopeApprovals}, RoleOperator},
		{"pairing_is_operator", []Scope{ScopePairing}, RoleOperator},
		{"read_is_viewer", []Scope{ScopeRead}, RoleViewer},
		{"empty_scopes", []Scope{}, RoleViewer},
		{"nil_scopes", nil, RoleViewer},
		{"provision_only_is_viewer", []Scope{ScopeProvision}, RoleViewer},
		{"read_and_write", []Scope{ScopeRead, ScopeWrite}, RoleOperator},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RoleFromScopes(tt.scopes)
			if got != tt.want {
				t.Fatalf("RoleFromScopes(%v) = %q, want %q", tt.scopes, got, tt.want)
			}
		})
	}
}

// --- CanAccess: role-based method access ---

func TestCanAccess_AdminMethods(t *testing.T) {
	pe := NewPolicyEngine(nil)
	adminMethods := []string{
		protocol.MethodConfigApply,
		protocol.MethodAgentsCreate,
		protocol.MethodAgentsDelete,
		protocol.MethodAPIKeysCreate,
		protocol.MethodTeamsCreate,
	}
	for _, method := range adminMethods {
		t.Run(method, func(t *testing.T) {
			if !pe.CanAccess(RoleAdmin, method) {
				t.Fatalf("admin should access %s", method)
			}
			if !pe.CanAccess(RoleOwner, method) {
				t.Fatalf("owner should access %s", method)
			}
			if pe.CanAccess(RoleOperator, method) {
				t.Fatalf("operator should NOT access %s", method)
			}
			if pe.CanAccess(RoleViewer, method) {
				t.Fatalf("viewer should NOT access %s", method)
			}
		})
	}
}

func TestCanAccess_WriteMethods(t *testing.T) {
	pe := NewPolicyEngine(nil)
	writeMethods := []string{
		protocol.MethodChatSend,
		protocol.MethodSessionsDelete,
		protocol.MethodCronCreate,
	}
	for _, method := range writeMethods {
		t.Run(method, func(t *testing.T) {
			if !pe.CanAccess(RoleOperator, method) {
				t.Fatalf("operator should access %s", method)
			}
			if !pe.CanAccess(RoleAdmin, method) {
				t.Fatalf("admin should access %s", method)
			}
			if pe.CanAccess(RoleViewer, method) {
				t.Fatalf("viewer should NOT access write method %s", method)
			}
		})
	}
}

func TestCanAccess_ReadMethods_AnyRole(t *testing.T) {
	pe := NewPolicyEngine(nil)
	// A method not in admin or write lists → defaults to viewer
	readMethod := "sessions.list" // not in admin/write lists
	for _, role := range []Role{RoleViewer, RoleOperator, RoleAdmin, RoleOwner} {
		if !pe.CanAccess(role, readMethod) {
			t.Fatalf("%s should access read method %s", role, readMethod)
		}
	}
}

// TestCanAccess_UnknownMethod_DeniedForAll locks in the fail-closed behavior
// introduced by issue #866: any method not present in the public / admin /
// write / read allowlists MUST be denied for every role including owner.
// This is the inverse of the previous default-permit behavior.
func TestCanAccess_UnknownMethod_DeniedForAll(t *testing.T) {
	pe := NewPolicyEngine(nil)
	unknown := "totally.unknown.method"
	for _, role := range []Role{RoleViewer, RoleOperator, RoleAdmin, RoleOwner} {
		if pe.CanAccess(role, unknown) {
			t.Fatalf("%s must NOT access unclassified method %q (fail-closed)", role, unknown)
		}
	}
	if MethodRole(unknown) != RoleNone {
		t.Fatalf("MethodRole(%q) = %q, want RoleNone", unknown, MethodRole(unknown))
	}
}

// TestCanAccess_CVE866_HeartbeatAndLogs asserts that the three RPCs exploited
// in the issue-#866 chain now require admin. Viewer/operator must be denied.
func TestCanAccess_CVE866_HeartbeatAndLogs(t *testing.T) {
	pe := NewPolicyEngine(nil)
	adminOnly := []string{
		protocol.MethodHeartbeatSet,
		protocol.MethodHeartbeatChecklistSet,
		protocol.MethodLogsTail,
		protocol.MethodHeartbeatToggle,
		protocol.MethodHeartbeatTest,
	}
	for _, method := range adminOnly {
		t.Run(method, func(t *testing.T) {
			if pe.CanAccess(RoleViewer, method) {
				t.Fatalf("viewer must NOT access %s (CVE #866)", method)
			}
			if pe.CanAccess(RoleOperator, method) {
				t.Fatalf("operator must NOT access %s (CVE #866)", method)
			}
			if !pe.CanAccess(RoleAdmin, method) {
				t.Fatalf("admin should access %s", method)
			}
			if !pe.CanAccess(RoleOwner, method) {
				t.Fatalf("owner should access %s", method)
			}
		})
	}
}

// TestCanAccess_PublicMethods ensures pre-auth RPCs remain reachable by every
// role (including the zero-value RoleNone placeholder that pre-connect clients
// hold) — otherwise legitimate clients cannot even complete the handshake.
func TestCanAccess_PublicMethods(t *testing.T) {
	pe := NewPolicyEngine(nil)
	public := []string{
		protocol.MethodConnect,
		protocol.MethodHealth,
		protocol.MethodStatus,
		protocol.MethodBrowserPairingStatus,
	}
	for _, method := range public {
		t.Run(method, func(t *testing.T) {
			for _, role := range []Role{RoleViewer, RoleOperator, RoleAdmin, RoleOwner} {
				if !pe.CanAccess(role, method) {
					t.Fatalf("%s must access public method %s", role, method)
				}
			}
		})
	}
}

// --- CanAccessWithScopes: scope-based method access ---

func TestCanAccessWithScopes(t *testing.T) {
	pe := NewPolicyEngine(nil)

	tests := []struct {
		name   string
		scopes []Scope
		method string
		want   bool
	}{
		// Admin method requires ScopeAdmin
		{"admin_scope_for_admin_method", []Scope{ScopeAdmin}, protocol.MethodAgentsCreate, true},
		{"read_scope_for_admin_method", []Scope{ScopeRead}, protocol.MethodAgentsCreate, false},
		{"write_scope_for_admin_method", []Scope{ScopeWrite}, protocol.MethodAgentsCreate, false},

		// Approvals method requires ScopeApprovals or ScopeAdmin
		{"approvals_scope_for_approvals", []Scope{ScopeApprovals}, "approvals.list", true},
		{"admin_scope_for_approvals", []Scope{ScopeAdmin}, "approvals.list", true},
		{"read_scope_for_approvals", []Scope{ScopeRead}, "approvals.list", false},

		// Write method requires ScopeWrite or ScopeAdmin
		{"write_scope_for_chat", []Scope{ScopeWrite}, protocol.MethodChatSend, true},
		{"admin_scope_for_chat", []Scope{ScopeAdmin}, protocol.MethodChatSend, true},
		{"read_scope_for_chat", []Scope{ScopeRead}, protocol.MethodChatSend, false},

		// Read method allows ScopeRead, ScopeWrite, or ScopeAdmin
		{"read_scope_for_read_method", []Scope{ScopeRead}, "sessions.list", true},
		{"write_scope_for_read_method", []Scope{ScopeWrite}, "sessions.list", true},
		{"admin_scope_for_read_method", []Scope{ScopeAdmin}, "sessions.list", true},

		// Empty scopes
		{"empty_scopes", []Scope{}, protocol.MethodAgentsCreate, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pe.CanAccessWithScopes(tt.scopes, tt.method)
			if got != tt.want {
				t.Fatalf("CanAccessWithScopes(%v, %q) = %v, want %v", tt.scopes, tt.method, got, tt.want)
			}
		})
	}
}

// --- IsOwner ---

func TestIsOwner(t *testing.T) {
	pe := NewPolicyEngine([]string{"alice", "bob"})

	if !pe.IsOwner("alice") {
		t.Fatal("alice should be owner")
	}
	if !pe.IsOwner("bob") {
		t.Fatal("bob should be owner")
	}
	if pe.IsOwner("charlie") {
		t.Fatal("charlie should not be owner")
	}
	if pe.IsOwner("") {
		t.Fatal("empty string should not be owner")
	}
}

func TestIsOwner_EmptyList(t *testing.T) {
	pe := NewPolicyEngine(nil)
	if pe.IsOwner("anyone") {
		t.Fatal("no one should be owner with empty list")
	}
}

// --- ValidScope ---

func TestValidScope(t *testing.T) {
	for scope := range AllScopes {
		if !ValidScope(string(scope)) {
			t.Fatalf("expected %q to be valid", scope)
		}
	}
	if ValidScope("nonexistent.scope") {
		t.Fatal("expected invalid scope to be rejected")
	}
	if ValidScope("") {
		t.Fatal("expected empty scope to be rejected")
	}
}

// --- MethodApprovalsList: locks in fix for writePrefixes shadowing bug.
// Before the fix, the "exec.approval." prefix in isWriteMethod's writePrefixes
// short-circuited the public→admin→write→read ordering in MethodRole,
// wrongly classifying exec.approval.list as RoleOperator. exec.approval.list
// is an explicit entry in isReadMethod and must resolve to RoleViewer.

func TestMethodRole_ApprovalsList_IsViewer(t *testing.T) {
	if got := MethodRole(protocol.MethodApprovalsList); got != RoleViewer {
		t.Fatalf("exec.approval.list must be RoleViewer (listed in isReadMethod); got %q", got)
	}
	if got := MethodRole(protocol.MethodApprovalsApprove); got != RoleOperator {
		t.Fatalf("exec.approval.approve must be RoleOperator; got %q", got)
	}
	if got := MethodRole(protocol.MethodApprovalsDeny); got != RoleOperator {
		t.Fatalf("exec.approval.deny must be RoleOperator; got %q", got)
	}
}

// --- Drift coverage: parses pkg/protocol/methods.go at test time, enumerates
// every const Method* = "...", and asserts none resolve to RoleNone. New RPCs
// added without a matching allowlist entry will be caught here before shipping
// as fail-closed rejections in production.

func TestMethodRole_DriftCoverage_AllProtocolMethodsClassified(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	methodsGoPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "pkg", "protocol", "methods.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, methodsGoPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", methodsGoPath, err)
	}

	var methods []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Method") {
					continue
				}
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				methods = append(methods, strings.Trim(lit.Value, `"`))
			}
		}
	}

	if len(methods) < 100 {
		t.Fatalf("expected 100+ Method* constants, parser collected %d — methods.go layout may have changed", len(methods))
	}

	var unclassified []string
	for _, m := range methods {
		if MethodRole(m) == RoleNone {
			unclassified = append(unclassified, m)
		}
	}
	if len(unclassified) > 0 {
		t.Fatalf("RBAC drift — %d protocol method(s) resolve to RoleNone:\n  %s\n\nAdd each to isPublicMethod, isAdminMethod, isWriteMethod, or isReadMethod in policy.go.",
			len(unclassified), strings.Join(unclassified, "\n  "))
	}
}

// --- MethodScopes: verify pairing/approvals special routes ---

func TestMethodScopes_PairingMethod(t *testing.T) {
	scopes := MethodScopes("pairing.request")
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes for pairing method, got %d", len(scopes))
	}
	// Should require ScopePairing or ScopeAdmin
	hasPairing, hasAdmin := false, false
	for _, s := range scopes {
		if s == ScopePairing {
			hasPairing = true
		}
		if s == ScopeAdmin {
			hasAdmin = true
		}
	}
	if !hasPairing || !hasAdmin {
		t.Fatalf("pairing method should require [pairing, admin], got %v", scopes)
	}
}

func TestMethodScopes_ApprovalMethod(t *testing.T) {
	scopes := MethodScopes("approvals.list")
	hasScopeApprovals, hasAdmin := false, false
	for _, s := range scopes {
		if s == ScopeApprovals {
			hasScopeApprovals = true
		}
		if s == ScopeAdmin {
			hasAdmin = true
		}
	}
	if !hasScopeApprovals || !hasAdmin {
		t.Fatalf("approvals method should require [approvals, admin], got %v", scopes)
	}
}
