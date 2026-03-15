package pg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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
