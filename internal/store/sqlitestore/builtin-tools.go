//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteBuiltinToolStore implements store.BuiltinToolStore backed by SQLite.
type SQLiteBuiltinToolStore struct {
	db *sql.DB
}

func NewSQLiteBuiltinToolStore(db *sql.DB) *SQLiteBuiltinToolStore {
	return &SQLiteBuiltinToolStore{db: db}
}

const builtinToolSelectCols = `name, display_name, description, category, enabled, settings, requires, metadata, created_at, updated_at`

func (s *SQLiteBuiltinToolStore) List(ctx context.Context) ([]store.BuiltinToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+builtinToolSelectCols+` FROM builtin_tools ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *SQLiteBuiltinToolStore) ListEnabled(ctx context.Context) ([]store.BuiltinToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+builtinToolSelectCols+` FROM builtin_tools WHERE enabled = 1 ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *SQLiteBuiltinToolStore) Get(ctx context.Context, name string) (*store.BuiltinToolDef, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+builtinToolSelectCols+` FROM builtin_tools WHERE name = ?`, name)
	return s.scanTool(row)
}

func (s *SQLiteBuiltinToolStore) GetSettings(ctx context.Context, name string) (json.RawMessage, error) {
	var settings json.RawMessage
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM builtin_tools WHERE name = ?`, name).Scan(&settings)
	if err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *SQLiteBuiltinToolStore) Update(ctx context.Context, name string, updates map[string]any) error {
	allowed := make(map[string]any)
	if v, ok := updates["enabled"]; ok {
		allowed["enabled"] = v
	}
	if v, ok := updates["settings"]; ok {
		switch sv := v.(type) {
		case json.RawMessage:
			allowed["settings"] = []byte(sv)
		case map[string]any:
			b, err := json.Marshal(sv)
			if err != nil {
				return fmt.Errorf("marshal settings: %w", err)
			}
			allowed["settings"] = b
		case []byte:
			allowed["settings"] = sv
		case string:
			allowed["settings"] = []byte(sv)
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	allowed["updated_at"] = time.Now()

	var setClauses []string
	var args []any
	for col, val := range allowed {
		setClauses = append(setClauses, col+" = ?")
		args = append(args, val)
	}
	args = append(args, name)
	q := fmt.Sprintf("UPDATE builtin_tools SET %s WHERE name = ?", strings.Join(setClauses, ", "))
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// Seed inserts or updates builtin tool definitions.
// Preserves user-customized enabled and settings values across upgrades.
func (s *SQLiteBuiltinToolStore) Seed(ctx context.Context, tools []store.BuiltinToolDef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO builtin_tools (name, display_name, description, category, enabled, settings, requires, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET
		   display_name = excluded.display_name,
		   description = excluded.description,
		   category = excluded.category,
		   requires = excluded.requires,
		   metadata = excluded.metadata,
		   settings = CASE
		     WHEN builtin_tools.settings IS NULL OR builtin_tools.settings IN ('{}', 'null')
		     THEN excluded.settings
		     ELSE builtin_tools.settings
		   END,
		   updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("prepare seed stmt: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		settings := t.Settings
		if settings == nil {
			settings = json.RawMessage("{}")
		}
		metadata := t.Metadata
		if metadata == nil {
			metadata = json.RawMessage("{}")
		}
		_, err := stmt.ExecContext(ctx,
			t.Name, t.DisplayName, t.Description, t.Category,
			t.Enabled, []byte(settings), jsonStringArray(t.Requires), []byte(metadata), now, now,
		)
		if err != nil {
			return fmt.Errorf("seed tool %s: %w", t.Name, err)
		}
		names = append(names, t.Name)
	}

	// Reconcile: remove stale entries not in the current seed list.
	// SQLite has no ANY($1) — build NOT IN (?, ?, ...) instead.
	if len(names) > 0 {
		placeholders := strings.Repeat("?,", len(names))
		placeholders = placeholders[:len(placeholders)-1]
		delArgs := make([]any, len(names))
		for i, n := range names {
			delArgs[i] = n
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM builtin_tools WHERE name NOT IN (`+placeholders+`)`, delArgs...); err != nil {
			return fmt.Errorf("reconcile stale builtin tools: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM builtin_tools`); err != nil {
			return fmt.Errorf("reconcile stale builtin tools: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteBuiltinToolStore) scanTool(row *sql.Row) (*store.BuiltinToolDef, error) {
	var def store.BuiltinToolDef
	var settings []byte
	var requires []byte
	var metadata []byte
	createdAt, updatedAt := scanTimePair()

	err := row.Scan(
		&def.Name, &def.DisplayName, &def.Description, &def.Category,
		&def.Enabled, &settings, &requires, &metadata, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	def.CreatedAt = createdAt.Time
	def.UpdatedAt = updatedAt.Time

	if settings != nil {
		def.Settings = json.RawMessage(settings)
	}
	if metadata != nil {
		def.Metadata = json.RawMessage(metadata)
	}
	scanJSONStringArray(requires, &def.Requires)

	return &def, nil
}

func (s *SQLiteBuiltinToolStore) scanTools(rows *sql.Rows) ([]store.BuiltinToolDef, error) {
	defer rows.Close()
	var result []store.BuiltinToolDef
	for rows.Next() {
		var def store.BuiltinToolDef
		var settings []byte
		var requires []byte
		var metadata []byte
		createdAt, updatedAt := scanTimePair()

		if err := rows.Scan(
			&def.Name, &def.DisplayName, &def.Description, &def.Category,
			&def.Enabled, &settings, &requires, &metadata, createdAt, updatedAt,
		); err != nil {
			continue
		}
		def.CreatedAt = createdAt.Time
		def.UpdatedAt = updatedAt.Time

		if settings != nil {
			def.Settings = json.RawMessage(settings)
		}
		if metadata != nil {
			def.Metadata = json.RawMessage(metadata)
		}
		scanJSONStringArray(requires, &def.Requires)

		result = append(result, def)
	}
	return result, rows.Err()
}
