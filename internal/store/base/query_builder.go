package base

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Dialect abstracts SQL differences between PostgreSQL and SQLite.
type Dialect interface {
	// Placeholder returns a positional parameter placeholder.
	// PG: "$1", "$2", ... SQLite: "?", "?", ...
	Placeholder(n int) string
	// TransformValue converts a Go value for the dialect.
	// PG: identity. SQLite: marshals maps/slices to JSON strings.
	TransformValue(v any) any
	// SupportsReturning indicates whether the dialect supports RETURNING clauses.
	SupportsReturning() bool
}

// QueryScope mirrors store.QueryScope without importing store/.
// Callers extract scope from context and convert to this struct.
type QueryScope struct {
	TenantID  uuid.UUID
	ProjectID *uuid.UUID
}

// BuildMapUpdate builds a dynamic UPDATE query from a column->value map.
// Column names and table name are validated against ValidColumnName to prevent SQL injection.
// Auto-sets updated_at for tables listed in TablesWithUpdatedAt.
//
// Returns: query string, args slice, error.
// The WHERE clause is: WHERE id = <placeholder>.
func BuildMapUpdate(d Dialect, table string, id uuid.UUID, updates map[string]any) (string, []any, error) {
	if len(updates) == 0 {
		return "", nil, nil
	}
	if !ValidColumnName.MatchString(table) {
		return "", nil, fmt.Errorf("invalid table name: %q", table)
	}
	var setClauses []string
	var args []any
	i := 1
	for col, val := range updates {
		if !ValidColumnName.MatchString(col) {
			return "", nil, fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", col, d.Placeholder(i)))
		args = append(args, d.TransformValue(val))
		i++
	}
	if _, ok := updates["updated_at"]; !ok && TableHasUpdatedAt(table) {
		setClauses = append(setClauses, fmt.Sprintf("updated_at = %s", d.Placeholder(i)))
		args = append(args, time.Now().UTC())
		i++
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = %s",
		table, strings.Join(setClauses, ", "), d.Placeholder(i))
	return q, args, nil
}

// BuildMapUpdateWhereTenant builds a dynamic UPDATE with both id and tenant_id in WHERE.
// Column names and table name are validated to prevent SQL injection.
// Auto-sets updated_at for tables listed in TablesWithUpdatedAt (matches execMapUpdate behavior).
func BuildMapUpdateWhereTenant(d Dialect, table string, updates map[string]any, id, tenantID uuid.UUID) (string, []any, error) {
	if len(updates) == 0 {
		return "", nil, nil
	}
	if !ValidColumnName.MatchString(table) {
		return "", nil, fmt.Errorf("invalid table name: %q", table)
	}
	var setClauses []string
	var args []any
	i := 1
	for col, val := range updates {
		if !ValidColumnName.MatchString(col) {
			return "", nil, fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", col, d.Placeholder(i)))
		args = append(args, d.TransformValue(val))
		i++
	}
	if _, ok := updates["updated_at"]; !ok && TableHasUpdatedAt(table) {
		setClauses = append(setClauses, fmt.Sprintf("updated_at = %s", d.Placeholder(i)))
		args = append(args, time.Now().UTC())
		i++
	}
	args = append(args, id, tenantID)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = %s AND tenant_id = %s",
		table, strings.Join(setClauses, ", "), d.Placeholder(i), d.Placeholder(i+1))
	return q, args, nil
}

// BuildScopeClause generates WHERE conditions for tenant + optional project scope.
// Uses the Dialect for placeholder generation.
// Returns clause (e.g. " AND tenant_id = $3"), args, and nextParam.
func BuildScopeClause(d Dialect, scope QueryScope, startParam int) (string, []any, int) {
	clause := fmt.Sprintf(" AND tenant_id = %s", d.Placeholder(startParam))
	args := []any{scope.TenantID}
	next := startParam + 1

	if scope.ProjectID != nil {
		clause += fmt.Sprintf(" AND project_id = %s", d.Placeholder(next))
		args = append(args, *scope.ProjectID)
		next++
	}
	return clause, args, next
}

// BuildScopeClauseAlias generates WHERE conditions qualified with a table alias.
// SECURITY: alias is interpolated — callers MUST pass hardcoded string literals only.
func BuildScopeClauseAlias(d Dialect, scope QueryScope, startParam int, alias string) (string, []any, int) {
	for _, c := range alias {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return "", nil, startParam
		}
	}
	clause := fmt.Sprintf(" AND %s.tenant_id = %s", alias, d.Placeholder(startParam))
	args := []any{scope.TenantID}
	next := startParam + 1

	if scope.ProjectID != nil {
		clause += fmt.Sprintf(" AND %s.project_id = %s", alias, d.Placeholder(next))
		args = append(args, *scope.ProjectID)
		next++
	}
	return clause, args, next
}
