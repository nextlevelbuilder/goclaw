package pg

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

// PGConfigPermissionStore implements store.ConfigPermissionStore backed by Postgres.
// Includes a TTL cache for CheckPermission to avoid per-request DB queries.
type PGConfigPermissionStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]permCacheEntry // key: "agentID:userID"
}

func NewPGConfigPermissionStore(db *sql.DB) *PGConfigPermissionStore {
	return &PGConfigPermissionStore{db: db, cache: make(map[string]permCacheEntry)}
}

// InvalidateCache clears all cached permission entries.
func (s *PGConfigPermissionStore) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]permCacheEntry)
}

// CheckPermission evaluates deny-first, allow-second permission with Go-level wildcard matching.
func (s *PGConfigPermissionStore) CheckPermission(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error) {
	cacheKey := agentID.String() + ":" + userID

	// Check cache.
	s.mu.RLock()
	if entry, ok := s.cache[cacheKey]; ok && time.Since(entry.fetched) < permCacheTTL {
		s.mu.RUnlock()
		return evalPermRows(entry.rows, scope, configType), nil
	}
	s.mu.RUnlock()

	// Fetch from DB.
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope, config_type, permission FROM agent_config_permissions
		 WHERE agent_id = $1 AND user_id = $2`,
		agentID, userID,
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
		`INSERT INTO agent_config_permissions (agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		 ON CONFLICT (agent_id, scope, config_type, user_id) DO UPDATE SET
		        permission = EXCLUDED.permission,
		        granted_by = EXCLUDED.granted_by,
		        metadata = EXCLUDED.metadata,
		        updated_at = EXCLUDED.updated_at`,
		perm.AgentID, perm.Scope, perm.ConfigType, perm.UserID, perm.Permission, perm.GrantedBy, meta, now,
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *PGConfigPermissionStore) Revoke(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_config_permissions WHERE agent_id = $1 AND scope = $2 AND config_type = $3 AND user_id = $4`,
		agentID, scope, configType, userID,
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *PGConfigPermissionStore) List(ctx context.Context, agentID uuid.UUID, configType string) ([]store.ConfigPermission, error) {
	query := `SELECT id, agent_id, scope, config_type, user_id, permission, granted_by, metadata, created_at, updated_at
	          FROM agent_config_permissions WHERE agent_id = $1`
	args := []any{agentID}

	if configType != "" {
		query += ` AND config_type = $2`
		args = append(args, configType)
	}
	query += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
