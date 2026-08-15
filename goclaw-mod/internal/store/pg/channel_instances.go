package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGChannelInstanceStore implements store.ChannelInstanceStore backed by Postgres.
type PGChannelInstanceStore struct {
	db     *sql.DB
	encKey string
}

func NewPGChannelInstanceStore(db *sql.DB, encryptionKey string) *PGChannelInstanceStore {
	return &PGChannelInstanceStore{db: db, encKey: encryptionKey}
}

const channelInstanceSelectCols = `id, name, display_name, channel_type, agent_id,
 credentials, config, enabled, created_by, created_at, updated_at, tenant_id`

func (s *PGChannelInstanceStore) Create(ctx context.Context, inst *store.ChannelInstanceData) error {
	if err := store.ValidateUserID(inst.CreatedBy); err != nil {
		return err
	}
	if inst.ID == uuid.Nil {
		inst.ID = store.GenNewID()
	}

	// Encrypt credentials if provided
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

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO channel_instances (id, name, display_name, channel_type, agent_id,
		 credentials, config, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		inst.ID, inst.Name, inst.DisplayName, inst.ChannelType, inst.AgentID,
		credsBytes, jsonOrEmpty(inst.Config),
		inst.Enabled, inst.CreatedBy, now, now, tenantID,
	)
	return err
}

func (s *PGChannelInstanceStore) Get(ctx context.Context, id uuid.UUID) (*store.ChannelInstanceData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE id = $1`, id)
		return s.scanInstance(row)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return s.scanInstance(row)
}

func (s *PGChannelInstanceStore) GetByName(ctx context.Context, name string) (*store.ChannelInstanceData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE name = $1`, name)
		return s.scanInstance(row)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+channelInstanceSelectCols+` FROM channel_instances WHERE name = $1 AND tenant_id = $2`, name, tenantID)
	return s.scanInstance(row)
}

func (s *PGChannelInstanceStore) scanInstance(row *sql.Row) (*store.ChannelInstanceData, error) {
	var inst store.ChannelInstanceData
	var displayName *string
	var creds []byte
	var config *[]byte

	err := row.Scan(
		&inst.ID, &inst.Name, &displayName, &inst.ChannelType, &inst.AgentID,
		&creds, &config,
		&inst.Enabled, &inst.CreatedBy, &inst.CreatedAt, &inst.UpdatedAt, &inst.TenantID,
	)
	if err != nil {
		return nil, err
	}

	inst.DisplayName = derefStr(displayName)
	if config != nil {
		inst.Config = *config
	}

	// Decrypt credentials
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

func (s *PGChannelInstanceStore) scanInstances(rows *sql.Rows) ([]store.ChannelInstanceData, error) {
	defer rows.Close()
	var result []store.ChannelInstanceData
	for rows.Next() {
		var inst store.ChannelInstanceData
		var displayName *string
		var creds []byte
		var config *[]byte

		if err := rows.Scan(
			&inst.ID, &inst.Name, &displayName, &inst.ChannelType, &inst.AgentID,
			&creds, &config,
			&inst.Enabled, &inst.CreatedBy, &inst.CreatedAt, &inst.UpdatedAt, &inst.TenantID,
		); err != nil {
			continue
		}

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
	return result, nil
}

func (s *PGChannelInstanceStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	// Merge and encrypt credentials if present
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
				if err := json.Unmarshal(raw, &newCreds); err != nil {
					newCreds = nil
				}
			}
		}

		// Merge with existing credentials so partial updates don't wipe other fields
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

// loadExistingCreds reads and decrypts the current credentials for merging.
func (s *PGChannelInstanceStore) loadExistingCreds(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT credentials FROM channel_instances WHERE id = $1", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || len(raw) == 0 {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
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

func (s *PGChannelInstanceStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM channel_instances WHERE id = $1", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM channel_instances WHERE id = $1 AND tenant_id = $2", id, tid)
	return err
}

func (s *PGChannelInstanceStore) ListEnabled(ctx context.Context) ([]store.ChannelInstanceData, error) {
	query := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances WHERE enabled = true`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = $1`
		qArgs = append(qArgs, tenantID)
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func (s *PGChannelInstanceStore) ListAll(ctx context.Context) ([]store.ChannelInstanceData, error) {
	query := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		query += ` WHERE tenant_id = $1`
		qArgs = append(qArgs, tenantID)
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func buildChannelInstanceWhere(ctx context.Context, opts store.ChannelInstanceListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
			args = append(args, tenantID)
			argIdx++
		}
	}

	if opts.Search != "" {
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d ESCAPE '\\' OR display_name ILIKE $%d ESCAPE '\\' OR channel_type ILIKE $%d ESCAPE '\\')", argIdx, argIdx, argIdx))
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(opts.Search)
		args = append(args, "%"+escaped+"%")
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	return where, args
}

func (s *PGChannelInstanceStore) ListPaged(ctx context.Context, opts store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	where, args := buildChannelInstanceWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances` + where +
		fmt.Sprintf(" ORDER BY name OFFSET %d LIMIT %d", opts.Offset, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

func (s *PGChannelInstanceStore) CountInstances(ctx context.Context, opts store.ChannelInstanceListOpts) (int, error) {
	where, args := buildChannelInstanceWhere(ctx, opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_instances"+where, args...).Scan(&count)
	return count, err
}
