package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGSkillStore) CreateSkill(name, slug string, description *string, ownerID, visibility string, version int, filePath string, fileSize int64, fileHash *string) error {
	id := store.GenNewID()
	_, err := s.db.Exec(
		`INSERT INTO skills (id, name, slug, description, owner_id, visibility, version, status, file_path, file_size, file_hash, tenant_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, $10, $11, NOW(), NOW())`,
		id, name, slug, description, ownerID, visibility, version, filePath, fileSize, fileHash, store.MasterTenantID,
	)
	if err == nil {
		s.BumpVersion()
	}
	return err
}

func (s *PGSkillStore) UpdateSkill(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	var err error
	if store.IsCrossTenant(ctx) {
		err = execMapUpdate(ctx, s.db, "skills", id, updates)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required for update")
		} else {
			err = execMapUpdateWhereTenant(ctx, s.db, "skills", updates, id, tid)
		}
	}
	if err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

func (s *PGSkillStore) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	// Reject deletion of system skills
	var isSystem bool
	if err := s.db.QueryRowContext(ctx, "SELECT is_system FROM skills WHERE id = $1", id).Scan(&isSystem); err != nil {
		return fmt.Errorf("check skill: %w", err)
	}
	if isSystem {
		return fmt.Errorf("cannot delete system skill")
	}

	// Tenant-scoped delete: only delete skill owned by this tenant.
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		var skillTenantID uuid.UUID
		if err := s.db.QueryRowContext(ctx, "SELECT tenant_id FROM skills WHERE id = $1", id).Scan(&skillTenantID); err != nil {
			return fmt.Errorf("skill not found")
		}
		if skillTenantID != tid {
			return fmt.Errorf("skill not found")
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Cascade: remove all agent grants for this skill
	if _, err := tx.ExecContext(ctx, "DELETE FROM skill_agent_grants WHERE skill_id = $1", id); err != nil {
		return fmt.Errorf("delete skill grants: %w", err)
	}

	// Cascade: remove all user grants for this skill
	if _, err := tx.ExecContext(ctx, "DELETE FROM skill_user_grants WHERE skill_id = $1", id); err != nil {
		return fmt.Errorf("delete skill user grants: %w", err)
	}

	// Soft-delete the skill (use 'deleted' status, distinct from 'archived' which means missing deps)
	if _, err := tx.ExecContext(ctx, "UPDATE skills SET status = 'deleted' WHERE id = $1", id); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// tenantSlugAdvisoryLock returns a stable int64 lock key derived from tenant ID + slug,
// suitable for use with pg_advisory_xact_lock. Tenant-scoped so different tenants
// uploading the same slug don't contend on the same lock.
func tenantSlugAdvisoryLock(tenantID uuid.UUID, slug string) int64 {
	h := fnv.New64a()
	h.Write(tenantID[:])
	h.Write([]byte(slug))
	return int64(h.Sum64())
}

// CreateSkillManaged creates or updates a skill from upload parameters.
// It uses a transaction with an advisory lock on the slug to prevent concurrent
// callers from racing on version calculation and upsert.
// The RETURNING id clause ensures the actual row ID is returned (new insert or
// existing row on conflict), so callers always receive a valid ID.
func (s *PGSkillStore) CreateSkillManaged(ctx context.Context, p store.SkillCreateParams) (uuid.UUID, error) {
	if err := store.ValidateUserID(p.OwnerID); err != nil {
		return uuid.Nil, err
	}

	// Marshal frontmatter to JSON for DB storage
	fmJSON := []byte("{}")
	if len(p.Frontmatter) > 0 {
		if b, err := json.Marshal(p.Frontmatter); err == nil {
			fmJSON = b
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Resolve tenant_id: default to MasterTenantID if no tenant in context.
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	// Acquire advisory lock scoped to this transaction so concurrent calls for
	// the same (tenant, slug) pair serialize version calculation and the upsert atomically.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", tenantSlugAdvisoryLock(tenantID, p.Slug)); err != nil {
		return uuid.Nil, fmt.Errorf("advisory lock: %w", err)
	}

	// Compute next version atomically under the lock, scoped to tenant.
	var version int
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) + 1 FROM skills WHERE slug = $1 AND tenant_id = $2",
		p.Slug, tenantID,
	).Scan(&version); err != nil {
		return uuid.Nil, fmt.Errorf("get next version: %w", err)
	}

	id := store.GenNewID()
	var returnedID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`INSERT INTO skills (id, name, slug, description, owner_id, tenant_id, visibility, version, status, frontmatter, file_path, file_size, file_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9, $10, $11, $12, NOW(), NOW())
		 ON CONFLICT (tenant_id, slug) DO UPDATE SET
		   name = EXCLUDED.name, description = EXCLUDED.description,
		   version = EXCLUDED.version, frontmatter = EXCLUDED.frontmatter,
		   file_path = EXCLUDED.file_path,
		   file_size = EXCLUDED.file_size, file_hash = EXCLUDED.file_hash,
		   visibility = CASE WHEN skills.status IN ('archived', 'deleted') THEN 'private' ELSE skills.visibility END,
		   status = 'active', updated_at = NOW()
		 RETURNING id`,
		id, p.Name, p.Slug, p.Description, p.OwnerID, tenantID, p.Visibility, version,
		fmJSON, p.FilePath, p.FileSize, p.FileHash,
	).Scan(&returnedID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert skill: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return uuid.Nil, fmt.Errorf("commit: %w", err)
	}

	s.BumpVersion()
	// Generate embedding asynchronously
	desc := ""
	if p.Description != nil {
		desc = *p.Description
	}
	go s.generateEmbedding(context.Background(), p.Slug, p.Name, desc)

	return returnedID, nil
}

// GetSkillFilePath returns the filesystem path, version, and system flag for a skill by UUID.
func (s *PGSkillStore) GetSkillFilePath(ctx context.Context, id uuid.UUID) (filePath string, slug string, version int, isSystem bool, ok bool) {
	q := "SELECT file_path, slug, version, is_system FROM skills WHERE id = $1 AND status = 'active'"
	args := []any{id}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			tid = store.MasterTenantID
		}
		q += " AND (is_system = true OR tenant_id = $2)"
		args = append(args, tid)
	}
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&filePath, &slug, &version, &isSystem)
	return filePath, slug, version, isSystem, err == nil
}

// GetNextVersion returns the next version number for a skill slug, scoped to tenant.
// NOTE: This function has an inherent race condition — two concurrent callers
// for the same slug can receive the same version number. Use it only for
// informational purposes (e.g. display). For write paths, use CreateSkillManaged
// which computes the version atomically under a pg_advisory_xact_lock.
func (s *PGSkillStore) GetNextVersion(ctx context.Context, slug string) int {
	tid := tenantIDForInsert(ctx)
	var maxVersion int
	s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM skills WHERE slug = $1 AND tenant_id = $2", slug, tid).Scan(&maxVersion)
	return maxVersion + 1
}

// GetNextVersionLocked computes the next version atomically using an advisory lock.
// Safe for concurrent write paths (patch, create). Returns version and a cleanup func
// that MUST be called to release the lock (commits the transaction).
func (s *PGSkillStore) GetNextVersionLocked(ctx context.Context, slug string) (int, func() error, error) {
	tenantID := tenantIDForInsert(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", tenantSlugAdvisoryLock(tenantID, slug)); err != nil {
		tx.Rollback()
		return 0, nil, fmt.Errorf("advisory lock: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) + 1 FROM skills WHERE slug = $1 AND tenant_id = $2", slug, tenantID,
	).Scan(&version); err != nil {
		tx.Rollback()
		return 0, nil, fmt.Errorf("get next version: %w", err)
	}
	return version, func() error { return tx.Commit() }, nil
}

// ToggleSkill enables or disables a skill by UUID.
func (s *PGSkillStore) ToggleSkill(ctx context.Context, id uuid.UUID, enabled bool) error {
	q := `UPDATE skills SET enabled = $1, updated_at = NOW() WHERE id = $2`
	args := []any{enabled, id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += fmt.Sprintf(" AND tenant_id = $%d", len(args)+1)
			args = append(args, tid)
		}
	}

	_, err := s.db.ExecContext(ctx, q, args...)
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
