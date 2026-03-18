package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGCustomToolStore implements store.CustomToolStore backed by Postgres.
type PGCustomToolStore struct {
	db     *sql.DB
	encKey string
}

func NewPGCustomToolStore(db *sql.DB, encryptionKey string) *PGCustomToolStore {
	return &PGCustomToolStore{db: db, encKey: encryptionKey}
}

const customToolSelectCols = `id, name, description, parameters, command, working_dir,
 timeout_seconds, env, agent_id, enabled, created_by, created_at, updated_at`

func (s *PGCustomToolStore) Create(ctx context.Context, def *store.CustomToolDef) error {
	if err := store.ValidateUserID(def.CreatedBy); err != nil {
		return err
	}
	if def.ID == uuid.Nil {
		def.ID = store.GenNewID()
	}

	// Encrypt env if provided
	var envBytes []byte
	if len(def.Env) > 0 && s.encKey != "" {
		encrypted, err := crypto.Encrypt(string(def.Env), s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt env: %w", err)
		}
		envBytes = []byte(encrypted)
	} else {
		envBytes = def.Env
	}

	now := time.Now()
	def.CreatedAt = now
	def.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO custom_tools (id, name, description, parameters, command, working_dir,
		 timeout_seconds, env, agent_id, enabled, created_by, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		def.ID, def.Name, def.Description,
		jsonOrEmpty(def.Parameters),
		def.Command, nilStr(def.WorkingDir),
		def.TimeoutSeconds, envBytes,
		nilUUID(def.AgentID), def.Enabled,
		def.CreatedBy, now, now,
	)
	return err
}

func (s *PGCustomToolStore) Get(ctx context.Context, id uuid.UUID) (*store.CustomToolDef, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE id = $1`, id)
	return s.scanTool(row)
}

func (s *PGCustomToolStore) scanTool(row *sql.Row) (*store.CustomToolDef, error) {
	var def store.CustomToolDef
	var workingDir *string
	var agentID *uuid.UUID
	var params *[]byte // pgx workaround: can't scan NULL JSONB into *json.RawMessage
	var env []byte

	err := row.Scan(
		&def.ID, &def.Name, &def.Description, &params,
		&def.Command, &workingDir,
		&def.TimeoutSeconds, &env, &agentID,
		&def.Enabled, &def.CreatedBy, &def.CreatedAt, &def.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	def.WorkingDir = derefStr(workingDir)
	def.AgentID = agentID
	if params != nil {
		def.Parameters = *params
	}

	// Decrypt env
	if len(env) > 0 && s.encKey != "" {
		decrypted, err := crypto.Decrypt(string(env), s.encKey)
		if err != nil {
			slog.Warn("custom_tools: failed to decrypt env", "tool", def.Name, "error", err)
		} else {
			def.Env = []byte(decrypted)
		}
	} else {
		def.Env = env
	}

	return &def, nil
}

func (s *PGCustomToolStore) scanTools(rows *sql.Rows) ([]store.CustomToolDef, error) {
	defer rows.Close()
	var result []store.CustomToolDef
	for rows.Next() {
		var def store.CustomToolDef
		var workingDir *string
		var agentID *uuid.UUID
		var params *[]byte
		var env []byte

		if err := rows.Scan(
			&def.ID, &def.Name, &def.Description, &params,
			&def.Command, &workingDir,
			&def.TimeoutSeconds, &env, &agentID,
			&def.Enabled, &def.CreatedBy, &def.CreatedAt, &def.UpdatedAt,
		); err != nil {
			continue
		}

		def.WorkingDir = derefStr(workingDir)
		def.AgentID = agentID
		if params != nil {
			def.Parameters = *params
		}
		if len(env) > 0 && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(string(env), s.encKey); err == nil {
				def.Env = []byte(decrypted)
			}
		} else {
			def.Env = env
		}

		result = append(result, def)
	}
	return result, nil
}

func (s *PGCustomToolStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	// Encrypt env if present
	if envVal, ok := updates["env"]; ok {
		if envStr, isStr := envVal.(string); isStr && envStr != "" && s.encKey != "" {
			encrypted, err := crypto.Encrypt(envStr, s.encKey)
			if err != nil {
				return fmt.Errorf("encrypt env: %w", err)
			}
			updates["env"] = []byte(encrypted)
		}
	}
	updates["updated_at"] = time.Now()
	return execMapUpdate(ctx, s.db, "custom_tools", id, updates)
}

func (s *PGCustomToolStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM custom_tools WHERE id = $1", id)
	return err
}

func (s *PGCustomToolStore) ListGlobal(ctx context.Context) ([]store.CustomToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE agent_id IS NULL AND enabled = true ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *PGCustomToolStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]store.CustomToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE agent_id = $1 AND enabled = true ORDER BY name`, agentID)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *PGCustomToolStore) ListAll(ctx context.Context) ([]store.CustomToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE enabled = true ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func buildCustomToolWhere(opts store.CustomToolListOpts) (string, []any) {
	conditions := []string{"enabled = true"}
	var args []any
	argIdx := 1

	if opts.AgentID != nil {
		conditions = append(conditions, fmt.Sprintf("agent_id = $%d", argIdx))
		args = append(args, *opts.AgentID)
		argIdx++
	}
	if opts.Search != "" {
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d ESCAPE '\\' OR description ILIKE $%d ESCAPE '\\')", argIdx, argIdx))
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(opts.Search)
		args = append(args, "%"+escaped+"%")
	}

	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *PGCustomToolStore) ListPaged(ctx context.Context, opts store.CustomToolListOpts) ([]store.CustomToolDef, error) {
	where, args := buildCustomToolWhere(opts)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT ` + customToolSelectCols + ` FROM custom_tools` + where +
		fmt.Sprintf(" ORDER BY name OFFSET %d LIMIT %d", opts.Offset, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return s.scanTools(rows)
}

func (s *PGCustomToolStore) CountTools(ctx context.Context, opts store.CustomToolListOpts) (int, error) {
	where, args := buildCustomToolWhere(opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM custom_tools"+where, args...).Scan(&count)
	return count, err
}
