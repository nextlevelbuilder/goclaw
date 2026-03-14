package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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

func derefBytes(b *[]byte) []byte {
	if b == nil {
		return nil
	}
	return *b
}

// --- JSON helpers ---

func jsonOrEmpty(data []byte) []byte {
	if data == nil {
		return []byte("{}")
	}
	return data
}

func jsonOrNull(data json.RawMessage) any {
	if data == nil {
		return nil
	}
	return []byte(data)
}

// --- PostgreSQL array helpers ---

// pqStringArray converts a Go string slice to a PostgreSQL text[] literal.
func pqStringArray(arr []string) any {
	if arr == nil {
		return nil
	}
	return "{" + strings.Join(arr, ",") + "}"
}

// scanStringArray parses a PostgreSQL text[] column (scanned as []byte) into a Go string slice.
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
	*dest = strings.Split(s, ",")
}

// --- Dynamic UPDATE helper ---

// execMapUpdate builds and runs a dynamic UPDATE from a column→value map.
func execMapUpdate(ctx context.Context, db *sql.DB, table string, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	i := 1
	for col, val := range updates {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	// Auto-set updated_at for tables that have the column, unless caller already included it.
	if _, ok := updates["updated_at"]; !ok && tableHasUpdatedAt(table) {
		setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", i))
		args = append(args, time.Now().UTC())
		i++
	}
	// Auto-set updated_by from context (only if table supports it and not already set).
	if _, ok := updates["updated_by"]; !ok && tableHasUpdatedBy(table) {
		if uid := store.UserIDFromContext(ctx); uid != "" {
			setClauses = append(setClauses, fmt.Sprintf("updated_by = $%d", i))
			args = append(args, uid)
			i++
		}
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d", table, strings.Join(setClauses, ", "), i)
	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// tablesWithUpdatedAt lists tables that have an updated_at column.
var tablesWithUpdatedAt = map[string]bool{
	"agents": true, "llm_providers": true, "sessions": true,
	"channel_instances": true, "cron_jobs": true, "custom_tools": true,
	"skills": true, "mcp_servers": true, "agent_links": true,
	"agent_teams": true, "team_tasks": true, "builtin_tools": true, "team_workspace_files": true,
	"agent_context_files": true, "user_context_files": true,
	"user_agent_overrides": true, "config_secrets": true,
	"memory_documents": true, "memory_chunks": true, "embedding_cache": true,
}

func tableHasUpdatedAt(table string) bool {
	return tablesWithUpdatedAt[table]
}

// tablesWithUpdatedBy lists tables that have an updated_by column.
var tablesWithUpdatedBy = map[string]bool{
	"agents": true, "llm_providers": true, "sessions": true,
	"channel_instances": true, "cron_jobs": true, "custom_tools": true,
	"skills": true, "mcp_servers": true,
}

func tableHasUpdatedBy(table string) bool {
	return tablesWithUpdatedBy[table]
}

// --- Ownership filter helper ---

// ownerFilter returns a SQL AND clause and arg for non-admin ownership filtering.
// Returns active=false for admin contexts or when userID is empty.
// ownerCol: the column to filter on (e.g. "created_by", "owner_id", "user_id").
// startIdx: the $N positional param index for the argument.
func ownerFilter(ctx context.Context, ownerCol string, startIdx int) (clause string, arg any, active bool) {
	if store.IsAdminContext(ctx) {
		return "", nil, false
	}
	uid := store.UserIDFromContext(ctx)
	if uid == "" {
		return "", nil, false
	}
	return fmt.Sprintf("AND %s = $%d", ownerCol, startIdx), uid, true
}

// FilterSkillsByOwner filters skills for non-admin users (handler layer, not store layer).
// If ctx is admin, returns all. Otherwise, returns only skills owned by the context user,
// system skills, or public skills.
func FilterSkillsByOwner(ctx context.Context, skills []store.SkillInfo) []store.SkillInfo {
	if store.IsAdminContext(ctx) {
		return skills
	}
	uid := store.UserIDFromContext(ctx)
	if uid == "" {
		return skills // unauthenticated — return all (existing behavior)
	}
	var filtered []store.SkillInfo
	for _, sk := range skills {
		if sk.Author == uid || sk.IsSystem || sk.Visibility == "public" {
			filtered = append(filtered, sk)
		}
	}
	return filtered
}
