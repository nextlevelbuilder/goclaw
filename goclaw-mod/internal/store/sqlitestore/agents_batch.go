//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GetByKeys returns agents matching the given keys in a single query.
// Replaces PG's = ANY($1) with a dynamic IN (?, ?, ...) clause.
func (s *SQLiteAgentStore) GetByKeys(ctx context.Context, keys []string) ([]store.AgentData, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	var q string
	if store.IsCrossTenant(ctx) {
		q = `SELECT ` + agentSelectCols + `
			 FROM agents WHERE agent_key IN (` + placeholders + `) AND deleted_at IS NULL`
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("no tenant context for batch agent lookup")
		}
		q = `SELECT ` + agentSelectCols + `
			 FROM agents WHERE agent_key IN (` + placeholders + `) AND deleted_at IS NULL AND tenant_id = ?`
		args = append(args, tid)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch agent key lookup: %w", err)
	}
	defer rows.Close()
	return scanAgentRows(rows)
}

// GetByIDs returns agents matching the given UUIDs in a single query.
// Replaces PG's = ANY($1) with a dynamic IN (?, ?, ...) clause.
func (s *SQLiteAgentStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]store.AgentData, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	var q string
	if store.IsCrossTenant(ctx) {
		q = `SELECT ` + agentSelectCols + `
			 FROM agents WHERE id IN (` + placeholders + `) AND deleted_at IS NULL`
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("no tenant context for batch agent lookup")
		}
		q = `SELECT ` + agentSelectCols + `
			 FROM agents WHERE id IN (` + placeholders + `) AND deleted_at IS NULL AND tenant_id = ?`
		args = append(args, tid)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch agent ID lookup: %w", err)
	}
	defer rows.Close()
	return scanAgentRows(rows)
}
