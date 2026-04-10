package pg

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

// --- Nullable helpers (delegated to base/) ---

var (
	nilStr    = base.NilStr
	nilInt    = base.NilInt
	nilUUID   = base.NilUUID
	nilTime   = base.NilTime
	derefStr  = base.DerefStr
	derefInt  = base.DerefInt
	derefUUID = base.DerefUUID
	derefBytes = base.DerefBytes
)

// --- JSON helpers (delegated to base/) ---

var (
	jsonOrEmpty      = base.JsonOrEmpty
	jsonOrEmptyArray = base.JsonOrEmptyArray
	jsonOrNull       = base.JsonOrNull
)

// --- Column/table validation (delegated to base/) ---

var validColumnName = base.ValidColumnName

// --- PostgreSQL array helpers (PG-specific) ---

// pqStringArray converts a Go string slice to a PostgreSQL text[] literal.
// Each element is double-quoted and escaped to prevent array literal injection.
func pqStringArray(arr []string) any {
	if arr == nil {
		return nil
	}
	quoted := make([]string, len(arr))
	for i, s := range arr {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		quoted[i] = `"` + escaped + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

// scanStringArray parses a PostgreSQL text[] column (scanned as []byte) into a Go string slice.
// Handles both quoted and unquoted elements in PostgreSQL array literal format.
func scanStringArray(data []byte, dest *[]string) {
	if data == nil || len(data) == 0 {
		return
	}
	s := string(data)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return
	}

	// Parse PostgreSQL array format: {val1,"quoted,val",val3}
	var result []string
	i := 0
	for i < len(s) {
		if s[i] == '"' {
			// Quoted element: find closing quote (handle escaped quotes)
			i++ // skip opening quote
			var elem strings.Builder
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					elem.WriteByte(s[i+1])
					i += 2
				} else if s[i] == '"' {
					i++ // skip closing quote
					break
				} else {
					elem.WriteByte(s[i])
					i++
				}
			}
			result = append(result, elem.String())
		} else {
			// Unquoted element: read until comma
			j := strings.IndexByte(s[i:], ',')
			if j < 0 {
				result = append(result, s[i:])
				break
			}
			result = append(result, s[i:i+j])
			i += j
		}
		// Skip comma separator
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
	*dest = result
}

// --- Dynamic UPDATE helpers (using base.BuildMapUpdate) ---

// execMapUpdate builds and runs a dynamic UPDATE from a column→value map.
func execMapUpdate(ctx context.Context, db *sql.DB, table string, id uuid.UUID, updates map[string]any) error {
	query, args, err := base.BuildMapUpdate(pgDialect, table, id, updates)
	if err != nil {
		slog.Warn("security.invalid_column_name", "table", table, "error", err)
		return err
	}
	if query == "" {
		return nil
	}
	_, err = db.ExecContext(ctx, query, args...)
	return err
}

// execMapUpdateWhereTenant is like execMapUpdate but appends AND tenant_id = $N filter.
func execMapUpdateWhereTenant(ctx context.Context, db *sql.DB, table string, updates map[string]any, id, tenantID uuid.UUID) error {
	query, args, err := base.BuildMapUpdateWhereTenant(pgDialect, table, updates, id, tenantID)
	if err != nil {
		slog.Warn("security.invalid_column_name", "table", table, "error", err)
		return err
	}
	if query == "" {
		return nil
	}
	_, err = db.ExecContext(ctx, query, args...)
	return err
}

// tableHasUpdatedAt returns true if the table has an updated_at column.
var tableHasUpdatedAt = base.TableHasUpdatedAt

// --- Tenant filter helpers (delegated to base/) ---

// tenantIDForInsert returns the tenant UUID for INSERT operations.
func tenantIDForInsert(ctx context.Context) uuid.UUID {
	return base.TenantIDForInsert(store.TenantIDFromContext(ctx), store.MasterTenantID)
}

// requireTenantID returns the tenant UUID or an error if missing (fail-closed).
func requireTenantID(ctx context.Context) (uuid.UUID, error) {
	tid := store.TenantIDFromContext(ctx)
	if err := base.RequireTenantID(tid); err != nil {
		return uuid.Nil, err
	}
	return tid, nil
}

// --- Scope-based query helpers (thin wrappers around base/) ---

// scopeClause extracts QueryScope from context and generates WHERE conditions.
func scopeClause(ctx context.Context, startParam int) (clause string, args []any, nextParam int, err error) {
	scope, err := store.ScopeFromContext(ctx)
	if err != nil {
		return "", nil, startParam, err
	}
	bScope := base.QueryScope{TenantID: scope.TenantID, ProjectID: scope.ProjectID}
	clause, args, nextParam = base.BuildScopeClause(pgDialect, bScope, startParam)
	return clause, args, nextParam, nil
}

// scopeClauseAlias is like scopeClause but qualifies columns with a table alias.
// SECURITY: alias is interpolated into SQL — callers MUST pass hardcoded string literals only.
func scopeClauseAlias(ctx context.Context, startParam int, alias string) (clause string, args []any, nextParam int, err error) {
	scope, err := store.ScopeFromContext(ctx)
	if err != nil {
		return "", nil, startParam, err
	}
	bScope := base.QueryScope{TenantID: scope.TenantID, ProjectID: scope.ProjectID}
	clause, args, nextParam = base.BuildScopeClauseAlias(pgDialect, bScope, startParam, alias)
	return clause, args, nextParam, nil
}

