package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultManagedToolsCacheTTL = 5 * time.Minute

// PGManagedToolStore implements store.ManagedToolStore backed by Postgres.
type PGManagedToolStore struct {
	db      *sql.DB
	baseDir string
	mu      sync.RWMutex
	version atomic.Int64

	// List cache: cached result of ListManagedTools() with version + TTL validation
	listCache []store.ManagedToolInfo
	listVer   int64
	listTime  time.Time
	ttl       time.Duration
}

func NewPGManagedToolStore(db *sql.DB, baseDir string) *PGManagedToolStore {
	return &PGManagedToolStore{
		db:      db,
		baseDir: baseDir,
		ttl:     defaultManagedToolsCacheTTL,
	}
}

func (s *PGManagedToolStore) ListManagedTools() []store.ManagedToolInfo {
	currentVer := s.version.Load()

	s.mu.RLock()
	if s.listCache != nil && s.listVer == currentVer && time.Since(s.listTime) < s.ttl {
		result := s.listCache
		s.mu.RUnlock()
		return result
	}
	s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled,
		        frontmatter, file_path, file_size, file_hash, owner_id, runtime, entry_point,
		        created_at, updated_at
		 FROM managed_tools
		 WHERE status = 'active' OR is_system = true
		 ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.ManagedToolInfo
	for rows.Next() {
		info, err := scanManagedTool(rows)
		if err != nil {
			continue
		}
		result = append(result, info)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListManagedTools: rows iteration error", "error", err)
		return nil
	}

	s.mu.Lock()
	s.listCache = result
	s.listVer = currentVer
	s.listTime = time.Now()
	s.mu.Unlock()

	return result
}

func (s *PGManagedToolStore) GetManagedTool(slug string) (*store.ManagedToolInfo, bool) {
	var id uuid.UUID
	var name, vis, status, ownerID string
	var desc, fileHash, runtime, entryPoint *string
	var tags []string
	var version int
	var isSystem, enabled bool
	var fmRaw []byte
	var filePath string
	var fileSize int64
	var createdAt, updatedAt time.Time

	err := s.db.QueryRow(
		`SELECT id, name, slug, description, visibility, tags, version, is_system, status, enabled,
		        frontmatter, file_path, file_size, file_hash, owner_id, runtime, entry_point,
		        created_at, updated_at
		 FROM managed_tools WHERE slug = $1 AND status = 'active'`, slug,
	).Scan(&id, &name, &slug, &desc, &vis, pq.Array(&tags), &version, &isSystem, &status, &enabled,
		&fmRaw, &filePath, &fileSize, &fileHash, &ownerID, &runtime, &entryPoint,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, false
	}

	info := buildManagedToolInfo(id.String(), name, slug, desc, vis, tags, version, isSystem, status,
		enabled, fmRaw, filePath, fileSize, fileHash, ownerID, runtime, entryPoint, &createdAt, &updatedAt)
	return &info, true
}

func (s *PGManagedToolStore) GetManagedToolByID(id uuid.UUID) (store.ManagedToolInfo, bool) {
	var name, slug, vis, status, ownerID string
	var desc, fileHash, runtime, entryPoint *string
	var tags []string
	var version int
	var isSystem, enabled bool
	var fmRaw []byte
	var filePath string
	var fileSize int64
	var createdAt, updatedAt time.Time

	err := s.db.QueryRow(
		`SELECT name, slug, description, visibility, tags, version, is_system, status, enabled,
		        frontmatter, file_path, file_size, file_hash, owner_id, runtime, entry_point,
		        created_at, updated_at
		 FROM managed_tools WHERE id = $1`, id,
	).Scan(&name, &slug, &desc, &vis, pq.Array(&tags), &version, &isSystem, &status, &enabled,
		&fmRaw, &filePath, &fileSize, &fileHash, &ownerID, &runtime, &entryPoint,
		&createdAt, &updatedAt)
	if err != nil {
		return store.ManagedToolInfo{}, false
	}

	return buildManagedToolInfo(id.String(), name, slug, desc, vis, tags, version, isSystem, status,
		enabled, fmRaw, filePath, fileSize, fileHash, ownerID, runtime, entryPoint, &createdAt, &updatedAt), true
}

func (s *PGManagedToolStore) CreateManagedTool(ctx context.Context, p store.ManagedToolCreateParams) (uuid.UUID, error) {
	if err := store.ValidateUserID(p.OwnerID); err != nil {
		return uuid.Nil, err
	}
	id := store.GenNewID()
	fmJSON := marshalFrontmatter(p.Frontmatter)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO managed_tools (id, name, slug, description, owner_id, visibility, version, status,
		 frontmatter, file_path, file_size, file_hash, runtime, entry_point, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, $10, $11, $12, $13, NOW(), NOW())
		 ON CONFLICT (slug) DO UPDATE SET
		   name = EXCLUDED.name, description = EXCLUDED.description,
		   version = EXCLUDED.version, frontmatter = EXCLUDED.frontmatter,
		   file_path = EXCLUDED.file_path,
		   file_size = EXCLUDED.file_size, file_hash = EXCLUDED.file_hash,
		   runtime = EXCLUDED.runtime, entry_point = EXCLUDED.entry_point,
		   visibility = CASE WHEN managed_tools.status = 'archived' THEN 'private' ELSE managed_tools.visibility END,
		   status = 'active', updated_at = NOW()`,
		id, p.Name, p.Slug, p.Description, p.OwnerID, p.Visibility, p.Version,
		fmJSON, p.FilePath, p.FileSize, p.FileHash, p.Runtime, p.EntryPoint,
	)
	if err == nil {
		s.BumpVersion()
	}
	return id, err
}

func (s *PGManagedToolStore) UpdateManagedTool(id uuid.UUID, updates map[string]any) error {
	if err := execMapUpdate(context.Background(), s.db, "managed_tools", id, updates); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

func (s *PGManagedToolStore) DeleteManagedTool(id uuid.UUID) error {
	var isSystem bool
	if err := s.db.QueryRow("SELECT is_system FROM managed_tools WHERE id = $1", id).Scan(&isSystem); err != nil {
		return fmt.Errorf("check managed tool: %w", err)
	}
	if isSystem {
		return fmt.Errorf("cannot delete system managed tool")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM managed_tool_agent_grants WHERE managed_tool_id = $1", id); err != nil {
		return fmt.Errorf("delete managed tool grants: %w", err)
	}

	if _, err := tx.Exec("UPDATE managed_tools SET status = 'archived' WHERE id = $1", id); err != nil {
		return fmt.Errorf("archive managed tool: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

func (s *PGManagedToolStore) ToggleManagedTool(id uuid.UUID, enabled bool) error {
	_, err := s.db.Exec(
		`UPDATE managed_tools SET enabled = $1, updated_at = NOW() WHERE id = $2`,
		enabled, id,
	)
	if err == nil {
		s.BumpVersion()
	}
	return err
}

func (s *PGManagedToolStore) Version() int64 { return s.version.Load() }
func (s *PGManagedToolStore) BumpVersion()   { s.version.Store(time.Now().UnixMilli()) }
func (s *PGManagedToolStore) Dirs() []string { return []string{s.baseDir} }

// GetManagedToolFilePath returns the file path, slug, and version for a managed tool by ID.
func (s *PGManagedToolStore) GetManagedToolFilePath(id uuid.UUID) (filePath string, slug string, version int, ok bool) {
	t, found := s.GetManagedToolByID(id)
	if !found {
		return "", "", 0, false
	}
	return t.FilePath, t.Slug, t.Version, true
}

// GetNextVersion returns the next version number for a slug (max existing version + 1).
func (s *PGManagedToolStore) GetNextVersion(slug string) int {
	var maxVer int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM managed_tools WHERE slug = $1`, slug).Scan(&maxVer)
	if err != nil {
		return 1
	}
	return maxVer + 1
}

// --- Helpers ---

// scanManagedTool scans a managed_tools row from a *sql.Rows cursor.
func scanManagedTool(rows *sql.Rows) (store.ManagedToolInfo, error) {
	var id uuid.UUID
	var name, slug, vis, status, ownerID string
	var desc, fileHash, runtime, entryPoint *string
	var tags []string
	var version int
	var isSystem, enabled bool
	var fmRaw []byte
	var filePath string
	var fileSize int64
	var createdAt, updatedAt time.Time

	if err := rows.Scan(&id, &name, &slug, &desc, &vis, pq.Array(&tags), &version, &isSystem, &status, &enabled,
		&fmRaw, &filePath, &fileSize, &fileHash, &ownerID, &runtime, &entryPoint,
		&createdAt, &updatedAt); err != nil {
		return store.ManagedToolInfo{}, err
	}

	return buildManagedToolInfo(id.String(), name, slug, desc, vis, tags, version, isSystem, status,
		enabled, fmRaw, filePath, fileSize, fileHash, ownerID, runtime, entryPoint, &createdAt, &updatedAt), nil
}

func buildManagedToolInfo(id, name, slug string, desc *string, vis string, tags []string,
	version int, isSystem bool, status string, enabled bool, fmRaw []byte,
	filePath string, fileSize int64, fileHash *string, ownerID string,
	runtime, entryPoint *string, createdAt, updatedAt *time.Time) store.ManagedToolInfo {

	d := ""
	if desc != nil {
		d = *desc
	}

	var fm map[string]string
	if len(fmRaw) > 0 {
		_ = json.Unmarshal(fmRaw, &fm)
	}

	return store.ManagedToolInfo{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Description: d,
		Visibility:  vis,
		Tags:        tags,
		Version:     version,
		IsSystem:    isSystem,
		Status:      status,
		Enabled:     enabled,
		Runtime:     runtime,
		EntryPoint:  entryPoint,
		FilePath:    filePath,
		FileSize:    fileSize,
		FileHash:    fileHash,
		OwnerID:     ownerID,
		Frontmatter: fm,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}
