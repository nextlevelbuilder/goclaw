package base

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// testDialectPG is a minimal PG dialect for testing.
type testDialectPG struct{}

func (testDialectPG) Placeholder(n int) string   { return "$" + itoa(n) }
func (testDialectPG) TransformValue(v any) any    { return v }
func (testDialectPG) SupportsReturning() bool     { return true }

// testDialectSQLite is a minimal SQLite dialect for testing.
type testDialectSQLite struct{}

func (testDialectSQLite) Placeholder(_ int) string { return "?" }
func (testDialectSQLite) TransformValue(v any) any { return v }
func (testDialectSQLite) SupportsReturning() bool  { return false }

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func TestBuildMapUpdate_PG_Placeholder(t *testing.T) {
	id := uuid.New()
	updates := map[string]any{"name": "test"}
	q, args, err := BuildMapUpdate(testDialectPG{}, "skills", id, updates)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "$1") || !strings.Contains(q, "$") {
		t.Errorf("PG query missing $N placeholder: %s", q)
	}
	if !strings.HasPrefix(q, "UPDATE skills SET") {
		t.Errorf("unexpected query: %s", q)
	}
	// args: name value + updated_at (skills has it) + id
	if len(args) < 3 {
		t.Errorf("expected >=3 args, got %d", len(args))
	}
}

func TestBuildMapUpdate_SQLite_Placeholder(t *testing.T) {
	id := uuid.New()
	updates := map[string]any{"name": "test"}
	q, _, err := BuildMapUpdate(testDialectSQLite{}, "skills", id, updates)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "$") {
		t.Errorf("SQLite query should use ?, got: %s", q)
	}
	if !strings.Contains(q, "?") {
		t.Errorf("SQLite query missing ? placeholder: %s", q)
	}
}

func TestBuildMapUpdate_EmptyUpdates(t *testing.T) {
	q, args, err := BuildMapUpdate(testDialectPG{}, "agents", uuid.New(), nil)
	if err != nil || q != "" || args != nil {
		t.Errorf("empty updates should return zero values, got q=%q args=%v err=%v", q, args, err)
	}
}

func TestBuildMapUpdate_InvalidColumn(t *testing.T) {
	_, _, err := BuildMapUpdate(testDialectPG{}, "agents", uuid.New(), map[string]any{
		"valid_col":       "ok",
		"bad; DROP TABLE": "injection",
	})
	if err == nil {
		t.Error("expected error for invalid column name")
	}
}

func TestBuildMapUpdate_AutoUpdatedAt(t *testing.T) {
	id := uuid.New()
	q, args, err := BuildMapUpdate(testDialectPG{}, "agents", id, map[string]any{"name": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "updated_at") {
		t.Error("agents should auto-set updated_at")
	}
	// name + updated_at + id = 3 args
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d", len(args))
	}
}

func TestBuildMapUpdate_NoAutoUpdatedAt_UnknownTable(t *testing.T) {
	id := uuid.New()
	q, args, err := BuildMapUpdate(testDialectPG{}, "unknown_table", id, map[string]any{"col": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "updated_at") {
		t.Error("unknown table should NOT auto-set updated_at")
	}
	// col + id = 2 args
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuildMapUpdateWhereTenant_PG(t *testing.T) {
	id := uuid.New()
	tid := uuid.New()
	q, args, err := BuildMapUpdateWhereTenant(testDialectPG{}, "agents", map[string]any{"name": "x"}, id, tid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "tenant_id") {
		t.Error("missing tenant_id in WHERE clause")
	}
	// name + updated_at + id + tenantID = 4 args
	if len(args) != 4 {
		t.Errorf("expected 4 args, got %d", len(args))
	}
	// Last two args should be id and tenantID
	if args[len(args)-2] != id || args[len(args)-1] != tid {
		t.Error("last two args should be id and tenantID")
	}
}

func TestBuildScopeClause_PG(t *testing.T) {
	tid := uuid.New()
	scope := QueryScope{TenantID: tid}
	clause, args, next := BuildScopeClause(testDialectPG{}, scope, 3)
	if clause != " AND tenant_id = $3" {
		t.Errorf("clause = %q, want \" AND tenant_id = $3\"", clause)
	}
	if len(args) != 1 || args[0] != tid {
		t.Errorf("args = %v, want [%s]", args, tid)
	}
	if next != 4 {
		t.Errorf("next = %d, want 4", next)
	}
}

func TestBuildScopeClause_PG_WithProject(t *testing.T) {
	tid := uuid.New()
	pid := uuid.New()
	scope := QueryScope{TenantID: tid, ProjectID: &pid}
	clause, args, next := BuildScopeClause(testDialectPG{}, scope, 1)
	if !strings.Contains(clause, "tenant_id = $1") || !strings.Contains(clause, "project_id = $2") {
		t.Errorf("clause = %q, want tenant + project", clause)
	}
	if len(args) != 2 {
		t.Errorf("args len = %d, want 2", len(args))
	}
	if next != 3 {
		t.Errorf("next = %d, want 3", next)
	}
}

func TestBuildScopeClause_SQLite(t *testing.T) {
	tid := uuid.New()
	scope := QueryScope{TenantID: tid}
	clause, args, next := BuildScopeClause(testDialectSQLite{}, scope, 1)
	if clause != " AND tenant_id = ?" {
		t.Errorf("clause = %q, want \" AND tenant_id = ?\"", clause)
	}
	if len(args) != 1 {
		t.Errorf("args len = %d, want 1", len(args))
	}
	// SQLite ignores startParam for placeholders, but next should still advance
	if next != 2 {
		t.Errorf("next = %d, want 2", next)
	}
}

func TestBuildScopeClauseAlias_PG(t *testing.T) {
	tid := uuid.New()
	scope := QueryScope{TenantID: tid}
	clause, args, next := BuildScopeClauseAlias(testDialectPG{}, scope, 2, "a")
	if clause != " AND a.tenant_id = $2" {
		t.Errorf("clause = %q, want \" AND a.tenant_id = $2\"", clause)
	}
	if len(args) != 1 || next != 3 {
		t.Errorf("args=%v next=%d", args, next)
	}
}

func TestBuildScopeClauseAlias_InvalidAlias(t *testing.T) {
	scope := QueryScope{TenantID: uuid.New()}
	clause, _, _ := BuildScopeClauseAlias(testDialectPG{}, scope, 1, "a; DROP")
	if clause != "" {
		t.Error("invalid alias should return empty clause")
	}
}

func TestBuildMapUpdate_InvalidTable(t *testing.T) {
	_, _, err := BuildMapUpdate(testDialectPG{}, "bad; DROP", uuid.New(), map[string]any{"col": "v"})
	if err == nil {
		t.Error("expected error for invalid table name")
	}
}

func TestBuildMapUpdateWhereTenant_InvalidTable(t *testing.T) {
	_, _, err := BuildMapUpdateWhereTenant(testDialectPG{}, "bad; DROP", map[string]any{"col": "v"}, uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error for invalid table name")
	}
}

func TestBuildMapUpdateWhereTenant_SQLite(t *testing.T) {
	id := uuid.New()
	tid := uuid.New()
	q, args, err := BuildMapUpdateWhereTenant(testDialectSQLite{}, "agents", map[string]any{"name": "y"}, id, tid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "$") {
		t.Errorf("SQLite query should use ?, got: %s", q)
	}
	if !strings.Contains(q, "tenant_id = ?") {
		t.Errorf("missing tenant_id in WHERE: %s", q)
	}
	// name + updated_at + id + tenantID = 4
	if len(args) != 4 {
		t.Errorf("expected 4 args, got %d", len(args))
	}
}

func TestBuildScopeClauseAlias_PG_WithProject(t *testing.T) {
	tid := uuid.New()
	pid := uuid.New()
	scope := QueryScope{TenantID: tid, ProjectID: &pid}
	clause, args, next := BuildScopeClauseAlias(testDialectPG{}, scope, 5, "t")
	if !strings.Contains(clause, "t.tenant_id = $5") || !strings.Contains(clause, "t.project_id = $6") {
		t.Errorf("clause = %q", clause)
	}
	if len(args) != 2 {
		t.Errorf("args len = %d, want 2", len(args))
	}
	if next != 7 {
		t.Errorf("next = %d, want 7", next)
	}
}

func TestTenantIDForInsert_NonNil(t *testing.T) {
	tid := uuid.New()
	fallback := uuid.New()
	if got := TenantIDForInsert(tid, fallback); got != tid {
		t.Errorf("got %s, want %s", got, tid)
	}
}

func TestTenantIDForInsert_Nil(t *testing.T) {
	fallback := uuid.New()
	if got := TenantIDForInsert(uuid.Nil, fallback); got != fallback {
		t.Errorf("got %s, want fallback %s", got, fallback)
	}
}

func TestRequireTenantID_Valid(t *testing.T) {
	if err := RequireTenantID(uuid.New()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRequireTenantID_Nil(t *testing.T) {
	if err := RequireTenantID(uuid.Nil); err == nil {
		t.Error("expected error for nil tenant ID")
	}
}
