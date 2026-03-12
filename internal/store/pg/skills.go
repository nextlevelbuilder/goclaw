package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
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
	// Returns active + system skills (and disabled ones — admin UI needs to see them to toggle back).
	rows, err := s.db.Query(
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps, frontmatter FROM skills WHERE status = 'active' OR is_system = true ORDER BY name`)
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
		if err := rows.Scan(&id, &name, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &status, &enabled, &depsRaw, &fmRaw); err != nil {
			continue
		}
		info := buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir)
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
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled, deps FROM skills WHERE enabled = true ORDER BY name`)
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
		if err := rows.Scan(&id, &name, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &status, &enabled, &depsRaw); err != nil {
			continue
		}
		info := buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir)
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

func (s *PGSkillStore) LoadSkill(name string) (string, bool) {
	var slug string
	var version int
	err := s.db.QueryRow(
		"SELECT slug, version FROM skills WHERE slug = $1 AND status = 'active'", name,
	).Scan(&slug, &version)
	if err != nil {
		return "", false
	}
	content, err := readSkillContent(s.baseDir, slug, version)
	if err != nil {
		return "", false
	}
	return content, true
}

func (s *PGSkillStore) LoadForContext(allowList []string) string {
	skills := s.FilterSkills(allowList)
	if len(skills) == 0 {
		return ""
	}
	var parts []string
	for _, sk := range skills {
		content, ok := s.LoadSkill(sk.Name)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", sk.Name, content))
	}
	if len(parts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString("## Available Skills\n\n")
	for i, p := range parts {
		if i > 0 {
			result.WriteString("\n\n---\n\n")
		}
		result.WriteString(p)
	}
	return result.String()
}

func (s *PGSkillStore) BuildSummary(allowList []string) string {
	skills := s.FilterSkills(allowList)
	if len(skills) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString("<available_skills>\n")
	for _, sk := range skills {
		result.WriteString("  <skill>\n")
		result.WriteString(fmt.Sprintf("    <name>%s</name>\n", sk.Name))
		result.WriteString(fmt.Sprintf("    <description>%s</description>\n", sk.Description))
		result.WriteString(fmt.Sprintf("    <location>%s</location>\n", sk.Path))
		result.WriteString("  </skill>\n")
	}
	result.WriteString("</available_skills>")
	return result.String()
}

func (s *PGSkillStore) GetSkill(name string) (*store.SkillInfo, bool) {
	var id uuid.UUID
	var skillName, slug, visibility string
	var desc *string
	var tags []string
	var version int
	var isSystem bool
	err := s.db.QueryRow(
		"SELECT id, name, slug, description, visibility, tags, version, is_system FROM skills WHERE slug = $1 AND status = 'active'", name,
	).Scan(&id, &skillName, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem)
	if err != nil {
		return nil, false
	}
	info := buildSkillInfo(id.String(), skillName, slug, desc, version, s.baseDir)
	info.Visibility = visibility
	info.Tags = tags
	info.IsSystem = isSystem
	return &info, true
}

func (s *PGSkillStore) FilterSkills(allowList []string) []store.SkillInfo {
	all := s.ListSkills()
	var filtered []store.SkillInfo
	if allowList == nil {
		// No allowList → return all enabled skills (for agent injection)
		for _, sk := range all {
			if sk.Enabled {
				filtered = append(filtered, sk)
			}
		}
		return filtered
	}
	if len(allowList) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowed[name] = true
	}
	for _, sk := range all {
		if sk.Enabled && allowed[sk.Slug] {
			filtered = append(filtered, sk)
		}
	}
	return filtered
}

func (s *PGSkillStore) Version() int64 { return s.version.Load() }
func (s *PGSkillStore) BumpVersion()   { s.version.Store(time.Now().UnixMilli()) }
func (s *PGSkillStore) Dirs() []string { return []string{s.baseDir} }

// --- CRUD for managed skill upload ---

func (s *PGSkillStore) CreateSkill(name, slug string, description *string, ownerID, visibility string, version int, filePath string, fileSize int64, fileHash *string) error {
	id := store.GenNewID()
	_, err := s.db.Exec(
		`INSERT INTO skills (id, name, slug, description, owner_id, visibility, version, status, file_path, file_size, file_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, $10, NOW(), NOW())`,
		id, name, slug, description, ownerID, visibility, version, filePath, fileSize, fileHash,
	)
	if err == nil {
		s.BumpVersion()
	}
	return err
}

func (s *PGSkillStore) UpdateSkill(id uuid.UUID, updates map[string]any) error {
	if err := execMapUpdate(context.Background(), s.db, "skills", id, updates); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

func (s *PGSkillStore) DeleteSkill(id uuid.UUID) error {
	// Reject deletion of system skills
	var isSystem bool
	if err := s.db.QueryRow("SELECT is_system FROM skills WHERE id = $1", id).Scan(&isSystem); err != nil {
		return fmt.Errorf("check skill: %w", err)
	}
	if isSystem {
		return fmt.Errorf("cannot delete system skill")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Cascade: remove all agent grants for this skill
	if _, err := tx.Exec("DELETE FROM skill_agent_grants WHERE skill_id = $1", id); err != nil {
		return fmt.Errorf("delete skill grants: %w", err)
	}

	// Cascade: remove all user grants for this skill
	if _, err := tx.Exec("DELETE FROM skill_user_grants WHERE skill_id = $1", id); err != nil {
		return fmt.Errorf("delete skill user grants: %w", err)
	}

	// Soft-delete the skill itself
	if _, err := tx.Exec("UPDATE skills SET status = 'archived' WHERE id = $1", id); err != nil {
		return fmt.Errorf("archive skill: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// SkillCreateParams holds parameters for creating a managed skill.
type SkillCreateParams struct {
	Name        string
	Slug        string
	Description *string
	OwnerID     string
	Visibility  string
	Status      string // "active" or "archived" (defaults to "active" if empty)
	Version     int
	FilePath    string
	FileSize    int64
	FileHash    *string
	Frontmatter map[string]string // parsed YAML frontmatter from SKILL.md
}

// CreateSkillManaged creates a skill from upload parameters.
func (s *PGSkillStore) CreateSkillManaged(ctx context.Context, p SkillCreateParams) (uuid.UUID, error) {
	if err := store.ValidateUserID(p.OwnerID); err != nil {
		return uuid.Nil, err
	}
	id := store.GenNewID()
	// Marshal frontmatter to JSON for DB storage
	fmJSON := []byte("{}")
	if len(p.Frontmatter) > 0 {
		if b, err := json.Marshal(p.Frontmatter); err == nil {
			fmJSON = b
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skills (id, name, slug, description, owner_id, visibility, version, status, frontmatter, file_path, file_size, file_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, $10, $11, NOW(), NOW())
		 ON CONFLICT (slug) DO UPDATE SET
		   name = EXCLUDED.name, description = EXCLUDED.description,
		   version = EXCLUDED.version, frontmatter = EXCLUDED.frontmatter,
		   file_path = EXCLUDED.file_path,
		   file_size = EXCLUDED.file_size, file_hash = EXCLUDED.file_hash,
		   visibility = CASE WHEN skills.status = 'archived' THEN 'private' ELSE skills.visibility END,
		   status = 'active', updated_at = NOW()`,
		id, p.Name, p.Slug, p.Description, p.OwnerID, p.Visibility, p.Version,
		fmJSON, p.FilePath, p.FileSize, p.FileHash,
	)
	if err == nil {
		s.BumpVersion()
		// Generate embedding asynchronously
		desc := ""
		if p.Description != nil {
			desc = *p.Description
		}
		go s.generateEmbedding(context.Background(), p.Slug, p.Name, desc)
	}
	return id, err
}

// GetSkillFilePath returns the filesystem path and version for a skill by UUID.
func (s *PGSkillStore) GetSkillFilePath(id uuid.UUID) (filePath string, slug string, version int, ok bool) {
	err := s.db.QueryRow(
		"SELECT file_path, slug, version FROM skills WHERE id = $1 AND status = 'active'", id,
	).Scan(&filePath, &slug, &version)
	return filePath, slug, version, err == nil
}

// GetNextVersion returns the next version number for a skill slug.
func (s *PGSkillStore) GetNextVersion(slug string) int {
	var maxVersion int
	s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM skills WHERE slug = $1", slug).Scan(&maxVersion)
	return maxVersion + 1
}

// UpsertSystemSkill creates or updates a system skill.
// Returns (id, changed, actualFilePath, error).
// When hash is unchanged, returns the existing file_path from DB so the caller
// uses the correct directory for dep scanning (not a non-existent next-version dir).
func (s *PGSkillStore) UpsertSystemSkill(ctx context.Context, p SkillCreateParams) (uuid.UUID, bool, string, error) {
	// Check if skill already exists
	var existingID uuid.UUID
	var existingHash *string
	var existingFilePath string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, file_hash, file_path FROM skills WHERE slug = $1", p.Slug,
	).Scan(&existingID, &existingHash, &existingFilePath)

	if err == nil {
		// Skill exists — check if hash changed
		if existingHash != nil && p.FileHash != nil && *existingHash == *p.FileHash {
			return existingID, false, existingFilePath, nil // unchanged, use existing path
		}
		// existingHash is nil (old record without hash) — backfill hash without bumping version
		if existingHash == nil && p.FileHash != nil {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE skills SET file_hash = $1, updated_at = NOW() WHERE id = $2`,
				p.FileHash, existingID,
			)
			return existingID, false, existingFilePath, nil
		}
		// Hash genuinely changed — full update with new version
		fmJSON := marshalFrontmatter(p.Frontmatter)
		_, err = s.db.ExecContext(ctx,
			`UPDATE skills SET name = $1, description = $2, version = $3, frontmatter = $4,
			 file_path = $5, file_size = $6, file_hash = $7, is_system = true,
			 visibility = 'public', status = $8, updated_at = NOW()
			 WHERE id = $9`,
			p.Name, p.Description, p.Version, fmJSON,
			p.FilePath, p.FileSize, p.FileHash, p.Status, existingID,
		)
		if err != nil {
			return uuid.Nil, false, "", fmt.Errorf("update system skill: %w", err)
		}
		s.BumpVersion()
		return existingID, true, p.FilePath, nil
	}

	// New skill — insert
	id := store.GenNewID()
	fmJSON := marshalFrontmatter(p.Frontmatter)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO skills (id, name, slug, description, owner_id, visibility, version, status,
		 is_system, frontmatter, file_path, file_size, file_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'system', 'public', $5, $6, true, $7, $8, $9, $10, NOW(), NOW())`,
		id, p.Name, p.Slug, p.Description, p.Version, p.Status,
		fmJSON, p.FilePath, p.FileSize, p.FileHash,
	)
	if err != nil {
		return uuid.Nil, false, "", fmt.Errorf("insert system skill: %w", err)
	}
	s.BumpVersion()
	// Generate embedding asynchronously
	desc := ""
	if p.Description != nil {
		desc = *p.Description
	}
	go s.generateEmbedding(context.Background(), p.Slug, p.Name, desc)
	return id, true, p.FilePath, nil
}

// ListSystemSkillDirs returns slug->file_path map for all enabled system skills.
// Disabled system skills are excluded — dep checking and injection are skipped for them.
func (s *PGSkillStore) ListSystemSkillDirs() map[string]string {
	rows, err := s.db.Query(
		`SELECT slug, file_path FROM skills WHERE is_system = true AND enabled = true`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	dirs := make(map[string]string)
	for rows.Next() {
		var slug, path string
		if err := rows.Scan(&slug, &path); err != nil {
			continue
		}
		dirs[slug] = path
	}
	return dirs
}

// IsSystemSkill checks if a skill slug belongs to a system skill.
func (s *PGSkillStore) IsSystemSkill(slug string) bool {
	var isSystem bool
	err := s.db.QueryRow("SELECT is_system FROM skills WHERE slug = $1", slug).Scan(&isSystem)
	return err == nil && isSystem
}

// GetSkillByID returns a SkillInfo for any skill by UUID, regardless of status or enabled flag.
// Used by admin operations (e.g. toggle) that need full skill info.
func (s *PGSkillStore) GetSkillByID(id uuid.UUID) (store.SkillInfo, bool) {
	var name, slug, visibility, status string
	var desc *string
	var tags []string
	var version int
	var isSystem, enabled bool
	var depsRaw []byte
	err := s.db.QueryRow(
		`SELECT name, slug, description, visibility, tags, version, is_system, status, enabled, deps
		 FROM skills WHERE id = $1`,
		id,
	).Scan(&name, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &status, &enabled, &depsRaw)
	if err != nil {
		return store.SkillInfo{}, false
	}
	info := buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir)
	info.Visibility = visibility
	info.Tags = tags
	info.IsSystem = isSystem
	info.Status = status
	info.Enabled = enabled
	info.MissingDeps = parseDepsColumn(depsRaw)
	return info, true
}

// ToggleSkill enables or disables a skill by UUID.
func (s *PGSkillStore) ToggleSkill(id uuid.UUID, enabled bool) error {
	_, err := s.db.Exec(
		`UPDATE skills SET enabled = $1, updated_at = NOW() WHERE id = $2`,
		enabled, id,
	)
	if err == nil {
		s.BumpVersion()
	}
	return err
}

// parseDepsColumn extracts the missing deps list from the deps JSONB column.
func parseDepsColumn(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var d struct {
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil
	}
	if len(d.Missing) == 0 {
		return nil
	}
	return d.Missing
}

func parseFrontmatterAuthor(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var fm map[string]string
	if err := json.Unmarshal(raw, &fm); err != nil {
		return ""
	}
	return fm["author"]
}

func marshalFrontmatter(fm map[string]string) []byte {
	if len(fm) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(fm)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// --- Embedding skill search (store.EmbeddingSkillSearcher) ---

// SetEmbeddingProvider sets the embedding provider for vector-based skill search.
func (s *PGSkillStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.embProvider = provider
}

// SearchByEmbedding performs vector similarity search over skills using pgvector cosine distance.
func (s *PGSkillStore) SearchByEmbedding(ctx context.Context, embedding []float32, limit int) ([]store.SkillSearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	vecStr := vectorToString(embedding)

	rows, err := s.db.QueryContext(ctx,
		`SELECT name, slug, COALESCE(description, ''), version,
				1 - (embedding <=> $1::vector) AS score
			FROM skills
			WHERE status = 'active' AND enabled = true AND embedding IS NOT NULL
			  AND visibility != 'private'
			ORDER BY embedding <=> $2::vector
			LIMIT $3`,
		vecStr, vecStr, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("embedding skill search: %w", err)
	}
	defer rows.Close()

	var results []store.SkillSearchResult
	for rows.Next() {
		var r store.SkillSearchResult
		var version int
		if err := rows.Scan(&r.Name, &r.Slug, &r.Description, &version, &r.Score); err != nil {
			continue
		}
		r.Path = fmt.Sprintf("%s/%s/%d/SKILL.md", s.baseDir, r.Slug, version)
		results = append(results, r)
	}
	return results, nil
}

// BackfillSkillEmbeddings generates embeddings for all active skills that don't have one yet.
func (s *PGSkillStore) BackfillSkillEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, '') FROM skills WHERE status = 'active' AND enabled = true AND embedding IS NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type skillRow struct {
		id   uuid.UUID
		name string
		desc string
	}
	var pending []skillRow
	for rows.Next() {
		var r skillRow
		if err := rows.Scan(&r.id, &r.name, &r.desc); err != nil {
			continue
		}
		pending = append(pending, r)
	}

	if len(pending) == 0 {
		return 0, nil
	}

	slog.Info("backfilling skill embeddings", "count", len(pending))
	updated := 0
	for _, sk := range pending {
		text := sk.name
		if sk.desc != "" {
			text += ": " + sk.desc
		}
		embeddings, err := s.embProvider.Embed(ctx, []string{text})
		if err != nil {
			slog.Warn("skill embedding failed", "skill", sk.name, "error", err)
			continue
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			continue
		}
		vecStr := vectorToString(embeddings[0])
		_, err = s.db.ExecContext(ctx,
			`UPDATE skills SET embedding = $1::vector WHERE id = $2`, vecStr, sk.id)
		if err != nil {
			slog.Warn("skill embedding update failed", "skill", sk.name, "error", err)
			continue
		}
		updated++
	}

	slog.Info("skill embeddings backfill complete", "updated", updated)
	return updated, nil
}

// generateEmbedding creates an embedding for a skill's name+description and stores it.
func (s *PGSkillStore) generateEmbedding(ctx context.Context, slug, name, description string) {
	if s.embProvider == nil {
		return
	}
	text := name
	if description != "" {
		text += ": " + description
	}
	embeddings, err := s.embProvider.Embed(ctx, []string{text})
	if err != nil {
		slog.Warn("skill embedding generation failed", "skill", name, "error", err)
		return
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return
	}
	vecStr := vectorToString(embeddings[0])
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET embedding = $1::vector WHERE slug = $2 AND status = 'active'`, vecStr, slug)
	if err != nil {
		slog.Warn("skill embedding store failed", "skill", name, "error", err)
	}
}

// --- Helpers ---

func buildSkillInfo(id, name, slug string, desc *string, version int, baseDir string) store.SkillInfo {
	d := ""
	if desc != nil {
		d = *desc
	}
	return store.SkillInfo{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Path:        fmt.Sprintf("%s/%s/%d/SKILL.md", baseDir, slug, version),
		BaseDir:     fmt.Sprintf("%s/%s/%d", baseDir, slug, version),
		Source:      "managed",
		Description: d,
		Version:     version,
	}
}

// skillFrontmatterRe matches YAML frontmatter (--- delimited) at the start of a file.
var skillFrontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)

func readSkillContent(baseDir, slug string, version int) (string, error) {
	path := fmt.Sprintf("%s/%s/%d/SKILL.md", baseDir, slug, version)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// Normalize line endings (Windows CRLF → LF) and strip frontmatter
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = skillFrontmatterRe.ReplaceAllString(content, "")
	return content, nil
}
