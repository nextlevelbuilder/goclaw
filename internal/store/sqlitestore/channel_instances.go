//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteChannelInstanceStore implements store.ChannelInstanceStore backed by SQLite.
type SQLiteChannelInstanceStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteChannelInstanceStore(db *sql.DB, encryptionKey string) *SQLiteChannelInstanceStore {
	return &SQLiteChannelInstanceStore{db: db, encKey: encryptionKey}
}

const channelInstanceSelectCols = `id, name, display_name, channel_type, agent_id,
 credentials, config, enabled, created_by, created_at, updated_at, tenant_id`

func (s *SQLiteChannelInstanceStore) Create(ctx context.Context, inst *store.ChannelInstanceData) error {
	if err := store.ValidateUserID(inst.CreatedBy); err != nil {
		return err
	}
	if inst.ID == uuid.Nil {
		inst.ID = store.GenNewID()
	}

	var credsBytes []byte
	if len(inst.Credentials) > 0 && s.encKey != "" {
		encrypted, err := crypto.Encrypt(string(inst.Credentials), s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt credentials: %w", err)
		}
		credsBytes = []byte(encrypted)
	} else {
		credsBytes = inst.Credentials
	}

	now := time.Now()
	inst.CreatedAt = now
	inst.UpdatedAt = now
	tid := tenantIDForInsert(ctx)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO channel_instances (id, name, display_name, channel_type, agent_id,
		 credentials, config, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		inst.ID, inst.Name, inst.DisplayName, inst.ChannelType, inst.AgentID,
		credsBytes, jsonOrEmpty(inst.Config),
		inst.Enabled, inst.CreatedBy, now, now, tid,
	)
	return err
}

func (s *SQLiteChannelInstanceStore) Get(ctx context.Context, id uuid.UUID) (*store.ChannelInstanceData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE id = ?`, id)
		return s.scanInstance(row)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{id}, tArgs...)
	row := s.db.QueryRowContext(ctx,
		`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE id = ?`+tClause, args...)
	return s.scanInstance(row)
}

func (s *SQLiteChannelInstanceStore) GetByName(ctx context.Context, name string) (*store.ChannelInstanceData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE name = ?`, name)
		return s.scanInstance(row)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{name}, tArgs...)
	row := s.db.QueryRowContext(ctx,
		`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE name = ?`+tClause, args...)
	return s.scanInstance(row)
}

func (s *SQLiteChannelInstanceStore) scanInstance(row *sql.Row) (*store.ChannelInstanceData, error) {
	var inst store.ChannelInstanceData
	var displayName *string
	var creds []byte
	var config *[]byte
	createdAt, updatedAt := scanTimePair()

	err := row.Scan(
		&inst.ID, &inst.Name, &displayName, &inst.ChannelType, &inst.AgentID,
		&creds, &config,
		&inst.Enabled, &inst.CreatedBy, createdAt, updatedAt, &inst.TenantID,
	)
	if err != nil {
		return nil, err
	}
	inst.CreatedAt = createdAt.Time
	inst.UpdatedAt = updatedAt.Time

	inst.DisplayName = derefStr(displayName)
	if config != nil {
		inst.Config = *config
	}

	if len(creds) > 0 && s.encKey != "" {
		decrypted, err := crypto.Decrypt(string(creds), s.encKey)
		if err != nil {
			slog.Warn("channel_instances: failed to decrypt credentials", "name", inst.Name, "error", err)
		} else {
			inst.Credentials = []byte(decrypted)
		}
	} else {
		inst.Credentials = creds
	}

	return &inst, nil
}

func (s *SQLiteChannelInstanceStore) scanInstances(rows *sql.Rows) ([]store.ChannelInstanceData, error) {
	defer rows.Close()
	var result []store.ChannelInstanceData
	for rows.Next() {
		var inst store.ChannelInstanceData
		var displayName *string
		var creds []byte
		var config *[]byte
		createdAt, updatedAt := scanTimePair()

		if err := rows.Scan(
			&inst.ID, &inst.Name, &displayName, &inst.ChannelType, &inst.AgentID,
			&creds, &config,
			&inst.Enabled, &inst.CreatedBy, createdAt, updatedAt, &inst.TenantID,
		); err != nil {
			continue
		}
		inst.CreatedAt = createdAt.Time
		inst.UpdatedAt = updatedAt.Time

		inst.DisplayName = derefStr(displayName)
		if config != nil {
			inst.Config = *config
		}
		if len(creds) > 0 && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(string(creds), s.encKey); err == nil {
				inst.Credentials = []byte(decrypted)
			}
		} else {
			inst.Credentials = creds
		}

		result = append(result, inst)
	}
	return result, rows.Err()
}

func (s *SQLiteChannelInstanceStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if credsVal, ok := updates["credentials"]; ok && credsVal != nil {
		var newCreds map[string]any
		switch v := credsVal.(type) {
		case map[string]any:
			newCreds = v
		default:
			var raw []byte
			switch vv := v.(type) {
			case []byte:
				raw = vv
			case string:
				raw = []byte(vv)
			default:
				if b, err := json.Marshal(v); err == nil {
					raw = b
				}
			}
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &newCreds)
			}
		}

		if len(newCreds) > 0 {
			existing, err := s.loadExistingCreds(ctx, id)
			if err != nil {
				return fmt.Errorf("load existing credentials for merge: %w", err)
			}
			maps.Copy(existing, newCreds)
			newCreds = existing
		}

		var credsBytes []byte
		if len(newCreds) > 0 {
			credsBytes, _ = json.Marshal(newCreds)
		}
		if len(credsBytes) > 0 && s.encKey != "" {
			encrypted, err := crypto.Encrypt(string(credsBytes), s.encKey)
			if err != nil {
				return fmt.Errorf("encrypt credentials: %w", err)
			}
			credsBytes = []byte(encrypted)
		}
		updates["credentials"] = credsBytes
	}
	updates["updated_at"] = time.Now()
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "channel_instances", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "channel_instances", updates, id, tid)
}

func (s *SQLiteChannelInstanceStore) loadExistingCreds(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT credentials FROM channel_instances WHERE id = ?", id).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return make(map[string]any), nil
	}
	if s.encKey != "" {
		if dec, err := crypto.Decrypt(string(raw), s.encKey); err == nil {
			raw = []byte(dec)
		}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return make(map[string]any), nil
	}
	return m, nil
}

func (s *SQLiteChannelInstanceStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM channel_instances WHERE id = ?", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM channel_instances WHERE id = ? AND tenant_id = ?", id, tid)
	return err
}

func (s *SQLiteChannelInstanceStore) ListEnabled(ctx context.Context) ([]store.ChannelInstanceData, error) {
	query := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances WHERE enabled = 1`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tClause, tArgs, err := scopeClause(ctx)
		if err != nil {
			return nil, err
		}
		query += tClause
		qArgs = tArgs
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func (s *SQLiteChannelInstanceStore) ListAll(ctx context.Context) ([]store.ChannelInstanceData, error) {
	query := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances WHERE 1=1`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tClause, tArgs, err := scopeClause(ctx)
		if err != nil {
			return nil, err
		}
		query += tClause
		qArgs = tArgs
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func (s *SQLiteChannelInstanceStore) ListAllInstances(ctx context.Context) ([]store.ChannelInstanceData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+channelInstanceSelectCols+` FROM channel_instances ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func (s *SQLiteChannelInstanceStore) ListAllEnabled(ctx context.Context) ([]store.ChannelInstanceData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func buildChannelInstanceWhereSQLite(ctx context.Context, opts store.ChannelInstanceListOpts) (string, []any) {
	var conditions []string
	var args []any

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, "tenant_id = ?")
			args = append(args, tenantID)
		}
	}

	if opts.Search != "" {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(opts.Search)
		conditions = append(conditions, "(name LIKE ? ESCAPE '\\' OR display_name LIKE ? ESCAPE '\\' OR channel_type LIKE ? ESCAPE '\\')")
		p := "%" + escaped + "%"
		args = append(args, p, p, p)
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	return where, args
}

func (s *SQLiteChannelInstanceStore) ListPaged(ctx context.Context, opts store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	where, args := buildChannelInstanceWhereSQLite(ctx, opts)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances` + where +
		fmt.Sprintf(" ORDER BY name LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func (s *SQLiteChannelInstanceStore) CountInstances(ctx context.Context, opts store.ChannelInstanceListOpts) (int, error) {
	where, args := buildChannelInstanceWhereSQLite(ctx, opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_instances"+where, args...).Scan(&count)
	return count, err
}
