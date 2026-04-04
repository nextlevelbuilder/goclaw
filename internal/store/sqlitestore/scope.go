//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// scopeClause extracts QueryScope from context and generates SQLite WHERE conditions
// using ? placeholders (instead of PG's $N positional params).
func scopeClause(ctx context.Context) (clause string, args []any, err error) {
	scope, err := store.ScopeFromContext(ctx)
	if err != nil {
		return "", nil, err
	}
	clause = " AND tenant_id = ?"
	args = []any{scope.TenantID}

	if scope.ProjectID != nil {
		clause += " AND project_id = ?"
		args = append(args, *scope.ProjectID)
	}
	return clause, args, nil
}

// scopeClauseAlias is like scopeClause but qualifies columns with a table alias.
// SECURITY: alias is interpolated — callers MUST pass hardcoded string literals only.
func scopeClauseAlias(ctx context.Context, alias string) (clause string, args []any, err error) {
	for _, c := range alias {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return "", nil, fmt.Errorf("invalid table alias: %q", alias)
		}
	}
	scope, err := store.ScopeFromContext(ctx)
	if err != nil {
		return "", nil, err
	}
	clause = " AND " + alias + ".tenant_id = ?"
	args = []any{scope.TenantID}

	if scope.ProjectID != nil {
		clause += " AND " + alias + ".project_id = ?"
		args = append(args, *scope.ProjectID)
	}
	return clause, args, nil
}
