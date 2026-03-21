package pg

import (
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

	// List cache: cached result of ListSkills() with version + TTL validation
	listCache []store.SkillInfo
	listVer   int64
	listTime  time.Time
	ttl       time.Duration

	// Embedding provider for vector-based skill search
	embProvider store.EmbeddingProvider
}

func NewPGSkillStore(db *sql.DB, baseDir string) *PGSkillStore {
	return &PGSkillStore{
		db:      db,
		baseDir: baseDir,
		cache:   make(map[string]*store.SkillInfo),
		ttl:     defaultSkillsCacheTTL,
	}
}

func (s *PGSkillStore) Version() int64 { return s.version.Load() }
func (s *PGSkillStore) BumpVersion()   { s.version.Store(time.Now().UnixMilli()) }
func (s *PGSkillStore) Dirs() []string { return []string{s.baseDir} }

func (s *PGSkillStore) ListSkills() []store.SkillInfo {
	currentVer := s.version.Load()

	s.mu.RLock()
	if s.listCache != nil && s.listVer == currentVer && time.Since(s.listTime) < s.ttl {
		result := s.listCache
		s.mu.RUnlock()
		return result
	}
	s.mu.RUnlock()

	// Cache miss or TTL expired → query DB
	// Returns active + archived + system skills. Archived skills are shown dimmed in the UI
	// so admins can see missing deps and re-activate after installing them.
	rows, err := s.db.Query(
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, frontmatter, file_path FROM skills WHERE status IN ('active', 'archived') OR is_system = true ORDER BY name`)
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
	s.listCache = result
	s.listVer = currentVer
	s.listTime = time.Now()
	s.mu.Unlock()

	return result
}

// ListAllSkills returns all enabled skills regardless of status (for admin operations like rescan-deps).
// Disabled skills are excluded — no point scanning or updating them.
func (s *PGSkillStore) ListAllSkills() []store.SkillInfo {
	rows, err := s.db.Query(
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, file_path FROM skills WHERE enabled = true AND status != 'deleted' ORDER BY name`)
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
		slog.Warn("ListAllSkills: rows iteration error", "error", err)
	}
	return result
}

// StoreMissingDeps persists the missing_deps list for a skill into the deps JSONB column.
func (s *PGSkillStore) StoreMissingDeps(id uuid.UUID, missing []string) error {
	if missing == nil {
		missing = []string{}
	}
	encoded, err := json.Marshal(map[string]any{"missing": missing})
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE skills SET deps = $1, updated_at = NOW() WHERE id = $2`,
		encoded, id,
	)
	if err == nil {
		s.BumpVersion()
	}
	return err
}
