package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// QueryScope represents the multi-level isolation scope for database queries.
// Currently supports tenant-level scoping; designed to be extended with
// project-level scoping when the Project feature is implemented.
type QueryScope struct {
	TenantID  uuid.UUID
	ProjectID *uuid.UUID // nil = no project filter (tenant-only)
}

// ScopeFromContext extracts the query scope from context.
// Returns error if tenant ID is missing (fail-closed).
func ScopeFromContext(ctx context.Context) (QueryScope, error) {
	tid := TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return QueryScope{}, fmt.Errorf("tenant_id required")
	}
	return QueryScope{TenantID: tid}, nil
}

// WhereClause generates SQL WHERE conditions for the scope.
// Returns the clause string (e.g. " AND tenant_id = $3"), args, and the next
// available parameter index. Callers chain: startParam → nextParam.
func (s QueryScope) WhereClause(startParam int) (clause string, args []any, nextParam int) {
	clause = fmt.Sprintf(" AND tenant_id = $%d", startParam)
	args = []any{s.TenantID}
	nextParam = startParam + 1

	if s.ProjectID != nil {
		clause += fmt.Sprintf(" AND project_id = $%d", nextParam)
		args = append(args, *s.ProjectID)
		nextParam++
	}

	return clause, args, nextParam
}

// WhereClauseAlias generates SQL WHERE conditions qualified with a table alias.
// Used in JOIN queries to avoid column ambiguity.
// SECURITY: alias is interpolated into SQL — callers MUST pass hardcoded string literals only.
func (s QueryScope) WhereClauseAlias(startParam int, alias string) (clause string, args []any, nextParam int) {
	// Defense-in-depth: only allow simple alphanumeric aliases to prevent SQL injection.
	for _, c := range alias {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return "", nil, startParam
		}
	}
	clause = fmt.Sprintf(" AND %s.tenant_id = $%d", alias, startParam)
	args = []any{s.TenantID}
	nextParam = startParam + 1

	if s.ProjectID != nil {
		clause += fmt.Sprintf(" AND %s.project_id = $%d", alias, nextParam)
		args = append(args, *s.ProjectID)
		nextParam++
	}

	return clause, args, nextParam
}

// InsertValues returns column names and values for INSERT operations.
// Falls back to MasterTenantID when TenantID is nil.
func (s QueryScope) InsertValues() (columns []string, values []any) {
	tid := s.TenantID
	if tid == uuid.Nil {
		tid = MasterTenantID
	}
	columns = []string{"tenant_id"}
	values = []any{tid}

	if s.ProjectID != nil {
		columns = append(columns, "project_id")
		values = append(values, *s.ProjectID)
	}

	return columns, values
}
