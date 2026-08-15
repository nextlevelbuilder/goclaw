package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGBuiltinToolStore implements store.BuiltinToolStore backed by Postgres.
type PGBuiltinToolStore struct {
	db *sql.DB
}

func NewPGBuiltinToolStore(db *sql.DB) *PGBuiltinToolStore {
	return &PGBuiltinToolStore{db: db}
}

const builtinToolSelectCols = `name, display_name, description, category, enabled, settings, requires, metadata, created_at, updated_at`

func (s *PGBuiltinToolStore) List(ctx context.Context) ([]store.BuiltinToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+builtinToolSelectCols+` FROM builtin_tools ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *PGBuiltinToolStore) ListEnabled(ctx context.Context) ([]store.BuiltinToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+builtinToolSelectCols+` FROM builtin_tools WHERE enabled = true ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *PGBuiltinToolStore) Get(ctx context.Context, name string) (*store.BuiltinToolDef, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+builtinToolSelectCols+` FROM builtin_tools WHERE name = $1`, name)
	return s.scanTool(row)
}

func (s *PGBuiltinToolStore) GetSettings(ctx context.Context, name string) (json.RawMessage, error) {
	var settings json.RawMessage
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM builtin_tools WHERE name = $1`, name).Scan(&settings)
	if err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *PGBuiltinToolStore) Update(ctx context.Context, name string, updates map[string]any) error {
	// Only allow updating enabled and settings
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

	// Build UPDATE with name as PK (not UUID)
	var setClauses []string
	var args []any
	i := 1
	for col, val := range allowed {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	args = append(args, name)
	q := fmt.Sprintf("UPDATE builtin_tools SET %s WHERE name = $%d", strings.Join(setClauses, ", "), i)
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// Seed inserts or updates builtin tool definitions.
// Preserves user-customized enabled and settings values across upgrades.
func (s *PGBuiltinToolStore) Seed(ctx context.Context, tools []store.BuiltinToolDef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO builtin_tools (name, display_name, description, category, enabled, settings, requires, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		 ON CONFLICT (name) DO UPDATE SET
		   display_name = EXCLUDED.display_name,
		   description = EXCLUDED.description,
		   category = EXCLUDED.category,
		   requires = EXCLUDED.requires,
		   metadata = EXCLUDED.metadata,
		   settings = CASE
		     WHEN builtin_tools.settings IS NULL OR builtin_tools.settings::text IN ('{}', 'null')
		     THEN EXCLUDED.settings
		     ELSE builtin_tools.settings
		   END,
		   updated_at = EXCLUDED.updated_at`)
	if err != nil {
		return fmt.Errorf("prepare seed stmt: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
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
			t.Enabled, []byte(settings), pqStringArray(t.Requires), []byte(metadata), now,
		)
		if err != nil {
			return fmt.Errorf("seed tool %s: %w", t.Name, err)
		}
	}

	// Reconcile: remove stale entries not in the current seed list
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM builtin_tools WHERE name != ALL($1)`, pqStringArray(names)); err != nil {
		return fmt.Errorf("reconcile stale builtin tools: %w", err)
	}

	return tx.Commit()
}

func (s *PGBuiltinToolStore) scanTool(row *sql.Row) (*store.BuiltinToolDef, error) {
	var def store.BuiltinToolDef
	var settings []byte
	var requires []byte
	var metadata []byte

	err := row.Scan(
		&def.Name, &def.DisplayName, &def.Description, &def.Category,
		&def.Enabled, &settings, &requires, &metadata, &def.CreatedAt, &def.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if settings != nil {
		def.Settings = json.RawMessage(settings)
	}
	if metadata != nil {
		def.Metadata = json.RawMessage(metadata)
	}
	scanStringArray(requires, &def.Requires)

	return &def, nil
}

func (s *PGBuiltinToolStore) scanTools(rows *sql.Rows) ([]store.BuiltinToolDef, error) {
	defer rows.Close()
	var result []store.BuiltinToolDef
	for rows.Next() {
		var def store.BuiltinToolDef
		var settings []byte
		var requires []byte
		var metadata []byte

		if err := rows.Scan(
			&def.Name, &def.DisplayName, &def.Description, &def.Category,
			&def.Enabled, &settings, &requires, &metadata, &def.CreatedAt, &def.UpdatedAt,
		); err != nil {
			continue
		}

		if settings != nil {
			def.Settings = json.RawMessage(settings)
		}
		if metadata != nil {
			def.Metadata = json.RawMessage(metadata)
		}
		scanStringArray(requires, &def.Requires)

		result = append(result, def)
	}
	return result, nil
}
