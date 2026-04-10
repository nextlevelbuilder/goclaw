//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

// scopeClause extracts QueryScope from context and generates SQLite WHERE conditions.
// Thin wrapper around base.BuildScopeClause with SQLite dialect.
func scopeClause(ctx context.Context) (clause string, args []any, err error) {
	scope, err := store.ScopeFromContext(ctx)
	if err != nil {
		return "", nil, err
	}
	bScope := base.QueryScope{TenantID: scope.TenantID, ProjectID: scope.ProjectID}
	clause, args, _ = base.BuildScopeClause(sqliteDialect, bScope, 0)
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
	bScope := base.QueryScope{TenantID: scope.TenantID, ProjectID: scope.ProjectID}
	clause, args, _ = base.BuildScopeClauseAlias(sqliteDialect, bScope, 0, alias)
	return clause, args, nil
}
