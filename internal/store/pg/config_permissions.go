package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const permCacheTTL = 60 * time.Second

// permCacheEntry holds cached permission rows for an agent+user pair.
type permCacheEntry struct {
	rows    []permRow
	fetched time.Time
}

type permRow struct {
	Scope      string
	ConfigType string
	Permission string
}

// fwCacheEntry holds cached file_writer ConfigPermission rows for a scope.
type fwCacheEntry struct {
	rows    []store.ConfigPermission
	fetched time.Time
}

// PGConfigPermissionStore implements store.ConfigPermissionStore backed by Postgres.
// Includes a TTL cache for CheckPermission to avoid per-request DB queries.
type PGConfigPermissionStore struct {
	db      *sql.DB
	mu      sync.RWMutex
	cache   map[string]permCacheEntry // key: "agentID:userID"
	fwMu    sync.RWMutex
	fwCache map[string]fwCacheEntry // key: "agentID:scope"
}

func NewPGConfigPermissionStore(db *sql.DB) *PGConfigPermissionStore {
	return &PGConfigPermissionStore{
		db:      db,
		cache:   make(map[string]permCacheEntry),
		fwCache: make(map[string]fwCacheEntry),
	}
}

// InvalidateCache clears all cached permission entries.
func (s *PGConfigPermissionStore) InvalidateCache() {
	s.mu.Lock()
	s.cache = make(map[string]permCacheEntry)
	s.mu.Unlock()

	s.fwMu.Lock()
	s.fwCache = make(map[string]fwCacheEntry)
	s.fwMu.Unlock()
}

// CheckPermission evaluates deny-first, allow-second permission with Go-level wildcard matching.
func (s *PGConfigPermissionStore) CheckPermission(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	cacheKey := tid.String() + ":" + agentID.String() + ":" + userID

	// Check cache.
	s.mu.RLock()
	if entry, ok := s.cache[cacheKey]; ok && time.Since(entry.fetched) < permCacheTTL {
		s.mu.RUnlock()
		return evalPermRows(entry.rows, scope, configType), nil
	}
	s.mu.RUnlock()

	// Fetch from DB.
	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return false, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope, config_type, permission FROM agent_config_permissions
		 WHERE agent_id = $1 AND user_id = $2`+tClause,
		append([]any{agentID, userID}, tArgs...)...,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var permRows []permRow
	for rows.Next() {
		var r permRow
		if err := rows.Scan(&r.Scope, &r.ConfigType, &r.Permission); err != nil {
			return false, err
		}
		permRows = append(permRows, r)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	// Update cache.
	s.mu.Lock()
	s.cache[cacheKey] = permCacheEntry{rows: permRows, fetched: time.Now()}
	s.mu.Unlock()

	return evalPermRows(permRows, scope, configType), nil
}

// evalPermRows evaluates cached permission rows against scope and configType.
func evalPermRows(rows []permRow, scope, configType string) bool {
	var hasDeny, hasAllow bool
	for _, r := range rows {
		if !matchWildcard(r.Scope, scope) || !matchWildcard(r.ConfigType, configType) {
			continue
		}
		switch r.Permission {
		case "deny":
			hasDeny = true
		case "allow":
			hasAllow = true
		}
	}
	if hasDeny {
		return false
	}
	return hasAllow
}

// matchWildcard performs simple wildcard matching for scope/config_type.
// Pattern examples: "*", "group:*", "group:telegram:*", "group:telegram:-100456"
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

func (s *PGConfigPermissionStore) Grant(ctx context.Context, perm *store.ConfigPermission) error {
	meta := perm.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_config_permissions (agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8,$9)
		 ON CONFLICT (agent_id, scope, config_type, user_id) DO UPDATE SET
		        permission = EXCLUDED.permission,
		        granted_by = EXCLUDED.granted_by,
		        metadata = EXCLUDED.metadata,
		        updated_at = EXCLUDED.updated_at`,
		perm.AgentID, perm.Scope, perm.ConfigType, perm.UserID, perm.Permission, perm.GrantedBy, meta, now, tenantIDForInsert(ctx),
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *PGConfigPermissionStore) Revoke(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) error {
	tClause, tArgs, err := tenantClauseN(ctx, 5)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM agent_config_permissions WHERE agent_id = $1 AND scope = $2 AND config_type = $3 AND user_id = $4`+tClause,
		append([]any{agentID, scope, configType, userID}, tArgs...)...,
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *PGConfigPermissionStore) List(ctx context.Context, agentID uuid.UUID, configType, scope string) ([]store.ConfigPermission, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 0) // paramN unused; we append manually
	if err != nil {
		return nil, err
	}
	query := `SELECT id, agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at
	          FROM agent_config_permissions WHERE agent_id = $1`
	args := []any{agentID}

	if configType != "" {
		args = append(args, configType)
		query += ` AND config_type = $` + itoa(len(args))
	}
	if scope != "" {
		args = append(args, scope)
		query += ` AND scope = $` + itoa(len(args))
	}
	if tClause != "" {
		args = append(args, tArgs...)
		query += fmt.Sprintf(" AND tenant_id = $%d", len(args))
	}
	query += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanConfigPermissions(rows)
}

// ListFileWriters returns cached file_writer allow permissions for a given agentID+scope.
// Hot-path: called during system prompt injection for every group message.
func (s *PGConfigPermissionStore) ListFileWriters(ctx context.Context, agentID uuid.UUID, scope string) ([]store.ConfigPermission, error) {
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

	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at
		 FROM agent_config_permissions
		 WHERE agent_id = $1 AND config_type = 'file_writer' AND scope = $2 AND permission = 'allow'`+tClause+`
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
		if err := rows.Scan(
			&p.ID, &p.AgentID, &p.Scope, &p.ConfigType, &p.UserID, &p.Permission, &p.GrantedBy, &metadata, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.Metadata = metadata
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return perms, nil
}

// itoa converts an int to its decimal string representation.
func itoa(n int) string {
	return strconv.Itoa(n)
}
