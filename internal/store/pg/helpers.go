package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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

func jsonOrNull(data json.RawMessage) interface{} {
	if data == nil {
		return nil
	}
	return []byte(data)
}

// --- PostgreSQL array helpers ---

// pqStringArray converts a Go string slice to a PostgreSQL text[] literal.
func pqStringArray(arr []string) interface{} {
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
	var args []interface{}
	i := 1
	for col, val := range updates {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d", table, strings.Join(setClauses, ", "), i)
	_, err := db.ExecContext(ctx, q, args...)
	return err
}
