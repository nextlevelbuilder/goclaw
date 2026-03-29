package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultSkillsCacheTTL = 5 * time.Minute

// PGSkillStore implements store.SkillStore backed by Postgres.
// Skills metadata lives in DB; content files on filesystem.
// ListSkills() is cached with version-based invalidation + TTL safety net.
// Also implements store.EmbeddingSkillSearcher for vector-based skill search.
type PGSkillStore struct {
	db      *sql.DB
	baseDir string // filesystem base for skill content
	mu      sync.RWMutex
	cache   map[string]*store.SkillInfo
	version atomic.Int64

	// List cache: per-tenant cached result of ListSkills() with version + TTL validation.
	// Key is tenant UUID; uuid.Nil = cross-tenant (system admin).
	listCache map[uuid.UUID]*listCacheEntry
	ttl       time.Duration

	// Embedding provider for vector-based skill search
	embProvider store.EmbeddingProvider
}

// listCacheEntry holds per-tenant cached skill list with version + TTL.
type listCacheEntry struct {
	skills []store.SkillInfo
	ver    int64
	time   time.Time
}

func NewPGSkillStore(db *sql.DB, baseDir string) *PGSkillStore {
	return &PGSkillStore{
		db:        db,
		baseDir:   baseDir,
		cache:     make(map[string]*store.SkillInfo),
		listCache: make(map[uuid.UUID]*listCacheEntry),
		ttl:       defaultSkillsCacheTTL,
	}
}

func (s *PGSkillStore) Version() int64 { return s.version.Load() }
func (s *PGSkillStore) BumpVersion()   { s.version.Store(time.Now().UnixMilli()) }
func (s *PGSkillStore) Dirs() []string { return []string{s.baseDir} }

func (s *PGSkillStore) ListSkills(ctx context.Context) []store.SkillInfo {
	currentVer := s.version.Load()
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}

	// Check per-tenant cache
	s.mu.RLock()
	if entry := s.listCache[tid]; entry != nil && entry.ver == currentVer && time.Since(entry.time) < s.ttl {
		result := entry.skills
		s.mu.RUnlock()
		return result
	}
	s.mu.RUnlock()

	// Cache miss or TTL expired → query DB
	// Returns active + archived + system skills. Archived skills are shown dimmed in the UI
	// so admins can see missing deps and re-activate after installing them.
	// Tenant filter: system skills visible globally, custom skills scoped to tenant.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, frontmatter, file_path
		 FROM skills WHERE (status IN ('active', 'archived') OR is_system = true) AND (is_system = true OR tenant_id = $1)
		 ORDER BY name`, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.SkillInfo
	for rows.Next() {
		var id uuid.UUID
		var name, slug, visibility, status string
		var desc *string
		var tags []string
		var version int
		var isSystem, enabled bool
		var depsRaw, fmRaw []byte
		var filePath *string
		if err := rows.Scan(&id, &name, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &status, &enabled, &depsRaw, &fmRaw, &filePath); err != nil {
			continue
		}
		info := buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir, filePath)
		info.Visibility = visibility
		info.Tags = tags
		info.IsSystem = isSystem
		info.Status = status
		info.Enabled = enabled
		info.MissingDeps = parseDepsColumn(depsRaw)
		info.Author = parseFrontmatterAuthor(fmRaw)
		result = append(result, info)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListSkills: rows iteration error", "error", err)
		return nil // don't cache partial results
	}

	s.mu.Lock()
	s.listCache[tid] = &listCacheEntry{skills: result, ver: currentVer, time: time.Now()}
	s.mu.Unlock()

	return result
}

// ListAllSkills returns system skills + custom skills for the given tenant (for admin operations like rescan-deps).
// Disabled skills are excluded — no point scanning or updating them.
func (s *PGSkillStore) ListAllSkills(ctx context.Context) []store.SkillInfo {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, file_path
		 FROM skills WHERE enabled = true AND status != 'deleted' AND (is_system = true OR tenant_id = $1)
		 ORDER BY name`, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	return s.scanSkillInfoList(rows)
}

// ListAllSystemSkills returns only system skills (for startup dependency scanning).
// No tenant filter — system skills belong to MasterTenantID and are globally visible.
func (s *PGSkillStore) ListAllSystemSkills(ctx context.Context) []store.SkillInfo {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, file_path
		 FROM skills WHERE is_system = true AND enabled = true AND status != 'deleted'
		 ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	return s.scanSkillInfoList(rows)
}

// scanSkillInfoList scans rows into a []SkillInfo slice. Shared by list methods.
func (s *PGSkillStore) scanSkillInfoList(rows *sql.Rows) []store.SkillInfo {
	var result []store.SkillInfo
	for rows.Next() {
		var id uuid.UUID
		var name, slug, visibility, status string
		var desc *string
		var tags []string
		var version int
		var isSystem, enabled bool
		var depsRaw []byte
		var filePath *string
		if err := rows.Scan(&id, &name, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &status, &enabled, &depsRaw, &filePath); err != nil {
			continue
		}
		info := buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir, filePath)
		info.Visibility = visibility
		info.Tags = tags
		info.IsSystem = isSystem
		info.Status = status
		info.Enabled = enabled
		info.MissingDeps = parseDepsColumn(depsRaw)
		result = append(result, info)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("scanSkillInfoList: rows iteration error", "error", err)
	}
	return result
}

// StoreMissingDeps persists the missing_deps list for a skill into the deps JSONB column.
// Works for both system and custom skills. System skills bypass tenant filter;
// custom skills require tenant_id match for cross-tenant safety.
func (s *PGSkillStore) StoreMissingDeps(ctx context.Context, id uuid.UUID, missing []string) error {
	encoded, err := marshalMissingDeps(missing)
	if err != nil {
		return err
	}
	tid := tenantIDForInsert(ctx)
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET deps = $1, updated_at = NOW() WHERE id = $2 AND (is_system = true OR tenant_id = $3)`,
		encoded, id, tid,
	)
	if err == nil {
		s.BumpVersion()
	}
	return err
}

func marshalMissingDeps(missing []string) ([]byte, error) {
	if missing == nil {
		missing = []string{}
	}
	return json.Marshal(map[string]any{"missing": missing})
}
