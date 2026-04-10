package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

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
	var scanned []skillInfoRowWithFrontmatter
	if err := pkgSqlxDB.SelectContext(ctx, &scanned,
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, frontmatter, file_path
		 FROM skills WHERE (status IN ('active', 'archived') OR is_system = true) AND (is_system = true OR tenant_id = $1)
		 ORDER BY name`, tid); err != nil {
		return nil
	}

	result := make([]store.SkillInfo, 0, len(scanned))
	for i := range scanned {
		result = append(result, scanned[i].toSkillInfo(s.baseDir))
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
	var scanned []skillInfoRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned,
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, file_path
		 FROM skills WHERE enabled = true AND status != 'deleted' AND (is_system = true OR tenant_id = $1)
		 ORDER BY name`, tid); err != nil {
		return nil
	}
	return skillInfoRowsToSlice(scanned, s.baseDir)
}

// ListAllSystemSkills returns only system skills (for startup dependency scanning).
// No tenant filter — system skills belong to MasterTenantID and are globally visible.
func (s *PGSkillStore) ListAllSystemSkills(ctx context.Context) []store.SkillInfo {
	var scanned []skillInfoRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned,
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, file_path
		 FROM skills WHERE is_system = true AND enabled = true AND status != 'deleted'
		 ORDER BY name`); err != nil {
		return nil
	}
	return skillInfoRowsToSlice(scanned, s.baseDir)
}

// skillInfoRowsToSlice converts a slice of skillInfoRow to []store.SkillInfo. Shared by list methods.
func skillInfoRowsToSlice(rows []skillInfoRow, baseDir string) []store.SkillInfo {
	result := make([]store.SkillInfo, len(rows))
	for i := range rows {
		result[i] = rows[i].toSkillInfo(baseDir)
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
