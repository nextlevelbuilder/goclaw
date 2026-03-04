package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	rows, err := s.db.Query(
		`SELECT id, name, slug, description, version FROM skills WHERE status = 'active' ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.SkillInfo
	for rows.Next() {
		var id uuid.UUID
		var name, slug string
		var desc *string
		var version int
		if err := rows.Scan(&id, &name, &slug, &desc, &version); err != nil {
			continue
		}
		result = append(result, buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir))
	}

	s.mu.Lock()
	s.listCache = result
	s.listVer = currentVer
	s.listTime = time.Now()
	s.mu.Unlock()

	return result
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
	result := "## Available Skills\n\n"
	for i, p := range parts {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += p
	}
	return result
}

func (s *PGSkillStore) BuildSummary(allowList []string) string {
	skills := s.FilterSkills(allowList)
	if len(skills) == 0 {
		return ""
	}
	result := "<available_skills>\n"
	for _, sk := range skills {
		result += "  <skill>\n"
		result += fmt.Sprintf("    <name>%s</name>\n", sk.Name)
		result += fmt.Sprintf("    <description>%s</description>\n", sk.Description)
		result += fmt.Sprintf("    <location>%s</location>\n", sk.Path)
		result += "  </skill>\n"
	}
	result += "</available_skills>"
	return result
}

func (s *PGSkillStore) GetSkill(name string) (*store.SkillInfo, bool) {
	var skillName, slug string
	var desc *string
	var version int
	err := s.db.QueryRow(
		"SELECT name, slug, description, version FROM skills WHERE slug = $1 AND status = 'active'", name,
	).Scan(&skillName, &slug, &desc, &version)
	if err != nil {
		return nil, false
	}
	info := buildSkillInfo("", skillName, slug, desc, version, s.baseDir)
	return &info, true
}

func (s *PGSkillStore) FilterSkills(allowList []string) []store.SkillInfo {
	all := s.ListSkills()
	if allowList == nil {
		return all
	}
	if len(allowList) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowed[name] = true
	}
	var filtered []store.SkillInfo
	for _, sk := range all {
		if allowed[sk.Slug] {
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

func (s *PGSkillStore) UpdateSkill(id uuid.UUID, updates map[string]interface{}) error {
	if err := execMapUpdate(context.Background(), s.db, "skills", id, updates); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

func (s *PGSkillStore) DeleteSkill(id uuid.UUID) error {
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

// GetNextVersion returns the next version number for a skill slug.
func (s *PGSkillStore) GetNextVersion(slug string) int {
	var maxVersion int
	s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM skills WHERE slug = $1", slug).Scan(&maxVersion)
	return maxVersion + 1
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
			WHERE status = 'active' AND embedding IS NOT NULL
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
		`SELECT id, name, COALESCE(description, '') FROM skills WHERE status = 'active' AND embedding IS NULL`)
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
	}
}

func readSkillContent(baseDir, slug string, version int) (string, error) {
	path := fmt.Sprintf("%s/%s/%d/SKILL.md", baseDir, slug, version)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
