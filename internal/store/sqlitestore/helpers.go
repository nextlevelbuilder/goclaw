//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// validColumnName matches safe SQL identifiers (letters, digits, underscores).
var validColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// --- Nullable helpers ---

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

func nilUUID(u *uuid.UUID) *uuid.UUID {
	if u == nil || *u == uuid.Nil {
		return nil
	}
	return u
}

func nilTime(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefUUID(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

// --- JSON helpers ---

func jsonOrEmpty(data []byte) []byte {
	if data == nil {
		return []byte("{}")
	}
	return data
}

func jsonOrEmptyArray(data []byte) []byte {
	if data == nil {
		return []byte("[]")
	}
	return data
}

func jsonOrNull(data json.RawMessage) any {
	if data == nil {
		return nil
	}
	// Return []byte for consistency with PG helpers (store implementations expect []byte).
	return []byte(data)
}

func derefBytes(b *[]byte) []byte {
	if b == nil {
		return nil
	}
	return *b
}

// jsonStringArray converts a Go string slice to a JSON array string for SQLite storage.
// SQLite stores arrays as JSON text (no native array type).
func jsonStringArray(arr []string) string {
	if arr == nil {
		return "[]"
	}
	data, _ := json.Marshal(arr)
	return string(data)
}

// scanJSONStringArray parses a JSON array stored as TEXT into a Go string slice.
func scanJSONStringArray(data []byte, dest *[]string) {
	if data == nil || len(data) == 0 {
		return
	}
	_ = json.Unmarshal(data, dest)
}

// sqliteVal marshals complex Go types (maps, slices) to JSON strings
// since the SQLite driver cannot serialize them directly.
func sqliteVal(v any) any {
	if v == nil {
		return nil
	}
	switch typed := v.(type) {
	case string, int, int64, float64, bool, time.Time, []byte, json.RawMessage:
		return v
	case *time.Time:
		if typed == nil {
			return nil
		}
		return *typed
	}
	// For maps, slices, etc. — marshal to JSON string.
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

// --- Dynamic UPDATE helper ---

// execMapUpdate builds and runs a dynamic UPDATE with ? placeholders.
func execMapUpdate(ctx context.Context, db *sql.DB, table string, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			slog.Warn("security.invalid_column_name", "table", table, "column", col)
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, sqliteVal(val))
	}
	// Auto-set updated_at for tables that have the column.
	if _, ok := updates["updated_at"]; !ok && tableHasUpdatedAt(table) {
		setClauses = append(setClauses, "updated_at = ?")
		args = append(args, time.Now().UTC())
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", table, strings.Join(setClauses, ", "))
	_, err := db.ExecContext(ctx, q, args...)
	return err
}

var tablesWithUpdatedAt = map[string]bool{
	"agents": true, "llm_providers": true, "sessions": true,
	"channel_instances": true, "cron_jobs": true,
	"skills": true, "mcp_servers": true, "agent_links": true,
	"agent_teams": true, "team_tasks": true, "builtin_tools": true,
	"agent_context_files": true, "user_context_files": true,
	"user_agent_overrides": true, "config_secrets": true,
	"memory_documents": true, "memory_chunks": true, "embedding_cache": true,
	"secure_cli_binaries": true, "tenants": true,
}

func tableHasUpdatedAt(table string) bool {
	return tablesWithUpdatedAt[table]
}

// --- Tenant filter helpers ---

func tenantIDForInsert(ctx context.Context) uuid.UUID {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.MasterTenantID
	}
	return tid
}

func requireTenantID(ctx context.Context) (uuid.UUID, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return uuid.Nil, fmt.Errorf("tenant_id required")
	}
	return tid, nil
}

// execMapUpdateWhereTenant builds and runs a dynamic UPDATE with ? placeholders,
// adding both id and tenant_id to the WHERE clause for tenant-scoped updates.
func execMapUpdateWhereTenant(ctx context.Context, db *sql.DB, table string, updates map[string]any, id, tenantID uuid.UUID) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			slog.Warn("security.invalid_column_name", "table", table, "column", col)
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, sqliteVal(val))
	}
	if _, ok := updates["updated_at"]; !ok && tableHasUpdatedAt(table) {
		setClauses = append(setClauses, "updated_at = ?")
		args = append(args, time.Now().UTC())
	}
	args = append(args, id, tenantID)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = ? AND tenant_id = ?",
		table, strings.Join(setClauses, ", "))
	_, err := db.ExecContext(ctx, q, args...)
	return err
}
