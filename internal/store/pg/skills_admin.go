package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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
		 is_system, frontmatter, file_path, file_size, file_hash, tenant_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'system', 'public', $5, $6, true, $7, $8, $9, $10, $11, NOW(), NOW())`,
		id, p.Name, p.Slug, p.Description, p.Version, p.Status,
		fmJSON, p.FilePath, p.FileSize, p.FileHash, store.MasterTenantID,
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
func (s *PGSkillStore) ListSystemSkillDirs(ctx context.Context) map[string]string {
	rows, err := s.db.QueryContext(ctx,
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
