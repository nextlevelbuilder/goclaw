//go:build sqliteonly

package backup

import (
	"context"
	"database/sql"
)

// DiscoverTenantTables is a no-op for SQLite builds.
// SQLite edition only has the master tenant — tenant backup is not supported.
func DiscoverTenantTables(_ context.Context, _ *sql.DB) (map[string]bool, error) {
	return nil, nil
}

// ValidateTableRegistry is a no-op for SQLite builds.
func ValidateTableRegistry(_ context.Context, _ *sql.DB) []string {
	return nil
}
