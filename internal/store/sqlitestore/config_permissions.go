//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const permCacheTTL = 60 * time.Second

type permRow struct {
	Scope      string
	ConfigType string
	Permission string
	UserID     string
}

type permCacheEntry struct {
	rows    []permRow
	fetched time.Time
}

type fwCacheEntry struct {
	rows    []store.ConfigPermission
	fetched time.Time
}

// SQLiteConfigPermissionStore implements store.ConfigPermissionStore backed by SQLite.
// Includes a TTL cache for CheckPermission to avoid per-request DB queries.
type SQLiteConfigPermissionStore struct {
	db      *sql.DB
	mu      sync.RWMutex
	cache   map[string]permCacheEntry // key: "tenantID:agentID:userID"
	fwMu    sync.RWMutex
	fwCache map[string]fwCacheEntry // key: "tenantID:agentID:scope"
}

func NewSQLiteConfigPermissionStore(db *sql.DB) *SQLiteConfigPermissionStore {
	return &SQLiteConfigPermissionStore{
		db:      db,
		cache:   make(map[string]permCacheEntry),
		fwCache: make(map[string]fwCacheEntry),
	}
}

// InvalidateCache clears all cached permission entries.
func (s *SQLiteConfigPermissionStore) InvalidateCache() {
	s.mu.Lock()
	s.cache = make(map[string]permCacheEntry)
	s.mu.Unlock()

	s.fwMu.Lock()
	s.fwCache = make(map[string]fwCacheEntry)
	s.fwMu.Unlock()
}

func (s *SQLiteConfigPermissionStore) CheckPermission(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	cacheKey := tid.String() + ":" + agentID.String() + ":" + userID

	s.mu.RLock()
	if entry, ok := s.cache[cacheKey]; ok && time.Since(entry.fetched) < permCacheTTL {
		s.mu.RUnlock()
		return evalPermRows(entry.rows, scope, configType, userID), nil
	}
	s.mu.RUnlock()

	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return false, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope, config_type, permission, user_id FROM agent_config_permissions
		 WHERE agent_id = ? AND (user_id = ? OR user_id = '*')`+tClause,
		append([]any{agentID, userID}, tArgs...)...,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var permRows []permRow
	for rows.Next() {
		var r permRow
		if err := rows.Scan(&r.Scope, &r.ConfigType, &r.Permission, &r.UserID); err != nil {
			return false, err
		}
		permRows = append(permRows, r)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	s.mu.Lock()
	s.cache[cacheKey] = permCacheEntry{rows: permRows, fetched: time.Now()}
	s.mu.Unlock()

	return evalPermRows(permRows, scope, configType, userID), nil
}

func (s *SQLiteConfigPermissionStore) Grant(ctx context.Context, perm *store.ConfigPermission) error {
	meta := perm.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_config_permissions (agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at, tenant_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (agent_id, scope, config_type, user_id) DO UPDATE SET
		        permission = excluded.permission,
		        granted_by = excluded.granted_by,
		        metadata = excluded.metadata,
		        updated_at = excluded.updated_at`,
		perm.AgentID, perm.Scope, perm.ConfigType, perm.UserID, perm.Permission, perm.GrantedBy, meta, now, now, tenantIDForInsert(ctx),
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *SQLiteConfigPermissionStore) Revoke(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM agent_config_permissions WHERE agent_id = ? AND scope = ? AND config_type = ? AND user_id = ?`+tClause,
		append([]any{agentID, scope, configType, userID}, tArgs...)...,
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *SQLiteConfigPermissionStore) List(ctx context.Context, agentID uuid.UUID, configType, scope string) ([]store.ConfigPermission, error) {
	query := `SELECT id, agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at
	          FROM agent_config_permissions WHERE agent_id = ?`
	args := []any{agentID}

	if configType != "" {
		query += " AND config_type = ?"
		args = append(args, configType)
	}
	if scope != "" {
		query += " AND scope = ?"
		args = append(args, scope)
	}

	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	if tClause != "" {
		query += tClause
		args = append(args, tArgs...)
	}
	query += " ORDER BY created_at"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanConfigPermissions(rows)
}

func (s *SQLiteConfigPermissionStore) ListFileWriters(ctx context.Context, agentID uuid.UUID, scope string) ([]store.ConfigPermission, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	cacheKey := tid.String() + ":" + agentID.String() + ":" + scope

	s.fwMu.RLock()
	if entry, ok := s.fwCache[cacheKey]; ok && time.Since(entry.fetched) < permCacheTTL {
		s.fwMu.RUnlock()
		return entry.rows, nil
	}
	s.fwMu.RUnlock()

	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at
		 FROM agent_config_permissions
		 WHERE agent_id = ? AND config_type = 'file_writer' AND scope = ? AND permission = 'allow'`+tClause+`
		 ORDER BY created_at`,
		append([]any{agentID, scope}, tArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perms, err := scanConfigPermissions(rows)
	if err != nil {
		return nil, err
	}

	s.fwMu.Lock()
	s.fwCache[cacheKey] = fwCacheEntry{rows: perms, fetched: time.Now()}
	s.fwMu.Unlock()

	return perms, nil
}

func scanConfigPermissions(rows *sql.Rows) ([]store.ConfigPermission, error) {
	var perms []store.ConfigPermission
	for rows.Next() {
		var p store.ConfigPermission
		var metadata []byte
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(
			&p.ID, &p.AgentID, &p.Scope, &p.ConfigType, &p.UserID, &p.Permission, &p.GrantedBy, &metadata, createdAt, updatedAt,
		); err != nil {
			return nil, err
		}
		p.CreatedAt = createdAt.Time
		p.UpdatedAt = updatedAt.Time
		p.Metadata = metadata
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return perms, nil
}

// evalPermRows evaluates cached permission rows with priority-based evaluation.
// Individual permissions (matching targetUserID) override group wildcards (user_id="*").
func evalPermRows(rows []permRow, scope, configType, targetUserID string) bool {
	var individualDeny, individualAllow bool
	var groupDeny, groupAllow bool

	for _, r := range rows {
		if !matchWildcard(r.Scope, scope) || !matchWildcard(r.ConfigType, configType) {
			continue
		}
		if r.UserID == targetUserID {
			switch r.Permission {
			case "deny":
				individualDeny = true
			case "allow":
				individualAllow = true
			}
		} else if r.UserID == "*" {
			switch r.Permission {
			case "deny":
				groupDeny = true
			case "allow":
				groupAllow = true
			}
		}
	}

	if individualDeny {
		return false
	}
	if individualAllow {
		return true
	}
	if groupDeny {
		return false
	}
	return groupAllow
}

// matchWildcard performs simple wildcard matching for scope/config_type.
func matchWildcard(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix) || value == strings.TrimSuffix(prefix, ":")
	}
	return pattern == value
}

// ensure interface is satisfied at compile time
var _ store.ConfigPermissionStore = (*SQLiteConfigPermissionStore)(nil)
