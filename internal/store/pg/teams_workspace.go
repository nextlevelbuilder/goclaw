package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// Workspace files
// ============================================================

func (s *PGTeamStore) UpsertWorkspaceFile(ctx context.Context, file *store.TeamWorkspaceFileData, diskWriteFn func(isNew bool) error) (bool, error) {
	if file.ID == uuid.Nil {
		file.ID = store.GenNewID()
	}
	now := time.Now()
	file.CreatedAt = now
	file.UpdatedAt = now
	if file.Tags == nil {
		file.Tags = []string{}
	}
	if len(file.Metadata) == 0 {
		file.Metadata = json.RawMessage(`{}`)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// Advisory lock on hash of team_id:chatID:file_name to prevent concurrent writes.
	lockKey := file.TeamID.String() + ":" + file.ChatID + ":" + file.FileName
	var locked bool
	if err := tx.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock(hashtext($1))`, lockKey,
	).Scan(&locked); err != nil {
		return false, err
	}
	if !locked {
		return false, store.ErrFileLocked
	}

	// Check if file already exists (scope: team_id + chat_id).
	var existingID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM team_workspace_files WHERE team_id = $1 AND chat_id = $2 AND file_name = $3`,
		file.TeamID, file.ChatID, file.FileName,
	).Scan(&existingID)

	isNew := err == sql.ErrNoRows
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	// Disk I/O under advisory lock — prevents concurrent writes to same file.
	if diskWriteFn != nil {
		if err := diskWriteFn(isNew); err != nil {
			return false, fmt.Errorf("disk write failed: %w", err)
		}
	}

	if isNew {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO team_workspace_files (id, team_id, channel, chat_id, file_name, mime_type, file_path, size_bytes, uploaded_by, task_id, pinned, tags, metadata, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			file.ID, file.TeamID, file.Channel, file.ChatID, file.FileName,
			sql.NullString{String: file.MimeType, Valid: file.MimeType != ""},
			file.FilePath, file.SizeBytes, file.UploadedBy, file.TaskID,
			file.Pinned, pq.Array(file.Tags), file.Metadata, now, now,
		)
	} else {
		file.ID = existingID
		_, err = tx.ExecContext(ctx,
			`UPDATE team_workspace_files SET file_path = $1, size_bytes = $2, mime_type = $3, uploaded_by = $4, task_id = $5, metadata = $6, updated_at = $7
			 WHERE id = $8`,
			file.FilePath, file.SizeBytes,
			sql.NullString{String: file.MimeType, Valid: file.MimeType != ""},
			file.UploadedBy, file.TaskID, file.Metadata, now, existingID,
		)
	}
	if err != nil {
		return false, err
	}

	return isNew, tx.Commit()
}

func (s *PGTeamStore) GetWorkspaceFile(ctx context.Context, teamID uuid.UUID, _, chatID, fileName string) (*store.TeamWorkspaceFileData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT f.id, f.team_id, f.channel, f.chat_id, f.file_name, f.mime_type, f.file_path, f.size_bytes,
		        f.uploaded_by, f.task_id, f.pinned, f.tags, f.metadata, f.archived_at, f.created_at, f.updated_at,
		        COALESCE(a.agent_key, '') AS uploaded_by_key
		 FROM team_workspace_files f
		 LEFT JOIN agents a ON a.id = f.uploaded_by
		 WHERE f.team_id = $1 AND f.chat_id = $2 AND f.file_name = $3`,
		teamID, chatID, fileName,
	)
	f, err := scanWorkspaceFile(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace file %q not found", fileName)
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *PGTeamStore) ListWorkspaceFiles(ctx context.Context, teamID uuid.UUID, _, chatID string) ([]store.TeamWorkspaceFileData, error) {
	// When chatID is empty the caller wants all files for the team (used by
	// the Workspace UI tab). When non-empty, filter to that user scope.
	var (
		rows *sql.Rows
		err  error
	)
	if chatID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT f.id, f.team_id, f.channel, f.chat_id, f.file_name, f.mime_type, f.file_path, f.size_bytes,
			        f.uploaded_by, f.task_id, f.pinned, f.tags, f.metadata, f.archived_at, f.created_at, f.updated_at,
			        COALESCE(a.agent_key, '') AS uploaded_by_key
			 FROM team_workspace_files f
			 LEFT JOIN agents a ON a.id = f.uploaded_by
			 WHERE f.team_id = $1 AND f.archived_at IS NULL
			 ORDER BY f.pinned DESC, f.updated_at DESC`,
			teamID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT f.id, f.team_id, f.channel, f.chat_id, f.file_name, f.mime_type, f.file_path, f.size_bytes,
			        f.uploaded_by, f.task_id, f.pinned, f.tags, f.metadata, f.archived_at, f.created_at, f.updated_at,
			        COALESCE(a.agent_key, '') AS uploaded_by_key
			 FROM team_workspace_files f
			 LEFT JOIN agents a ON a.id = f.uploaded_by
			 WHERE f.team_id = $1 AND f.chat_id = $2 AND f.archived_at IS NULL
			 ORDER BY f.pinned DESC, f.updated_at DESC`,
			teamID, chatID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkspaceFileRows(rows)
}

func (s *PGTeamStore) DeleteWorkspaceFile(ctx context.Context, teamID uuid.UUID, _, chatID, fileName string) (string, error) {
	var filePath string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM team_workspace_files WHERE team_id = $1 AND chat_id = $2 AND file_name = $3 RETURNING file_path`,
		teamID, chatID, fileName,
	).Scan(&filePath)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("workspace file %q not found", fileName)
	}
	return filePath, err
}

func (s *PGTeamStore) CountWorkspaceFiles(ctx context.Context, teamID uuid.UUID, _, chatID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_workspace_files WHERE team_id = $1 AND chat_id = $2 AND archived_at IS NULL`,
		teamID, chatID,
	).Scan(&count)
	return count, err
}

func (s *PGTeamStore) PinWorkspaceFile(ctx context.Context, teamID uuid.UUID, _, chatID, fileName string, pinned bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_workspace_files SET pinned = $1, updated_at = $2 WHERE team_id = $3 AND chat_id = $4 AND file_name = $5`,
		pinned, time.Now(), teamID, chatID, fileName,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace file %q not found", fileName)
	}
	return nil
}

func (s *PGTeamStore) TagWorkspaceFile(ctx context.Context, teamID uuid.UUID, _, chatID, fileName string, tags []string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_workspace_files SET tags = $1, updated_at = $2 WHERE team_id = $3 AND chat_id = $4 AND file_name = $5`,
		pq.Array(tags), time.Now(), teamID, chatID, fileName,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace file %q not found", fileName)
	}
	return nil
}

func (s *PGTeamStore) ListDeliverableFiles(ctx context.Context, teamID uuid.UUID, _, chatID string) ([]store.TeamWorkspaceFileData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.team_id, f.channel, f.chat_id, f.file_name, f.mime_type, f.file_path, f.size_bytes,
		        f.uploaded_by, f.task_id, f.pinned, f.tags, f.metadata, f.archived_at, f.created_at, f.updated_at,
		        COALESCE(a.agent_key, '') AS uploaded_by_key
		 FROM team_workspace_files f
		 LEFT JOIN agents a ON a.id = f.uploaded_by
		 WHERE f.team_id = $1 AND f.chat_id = $2 AND 'deliverable' = ANY(f.tags) AND f.archived_at IS NULL
		 ORDER BY f.updated_at DESC`,
		teamID, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkspaceFileRows(rows)
}

func (s *PGTeamStore) ArchiveWorkspaceFilesByTask(ctx context.Context, taskID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_workspace_files SET archived_at = $1, updated_at = $1 WHERE task_id = $2 AND NOT pinned AND archived_at IS NULL`,
		time.Now(), taskID,
	)
	return err
}

func (s *PGTeamStore) ListOrphanWorkspaceFiles(ctx context.Context, teamID uuid.UUID, olderThan time.Time) ([]store.TeamWorkspaceFileData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.team_id, f.channel, f.chat_id, f.file_name, f.mime_type, f.file_path, f.size_bytes,
		        f.uploaded_by, f.task_id, f.pinned, f.tags, f.metadata, f.archived_at, f.created_at, f.updated_at,
		        COALESCE(a.agent_key, '') AS uploaded_by_key
		 FROM team_workspace_files f
		 LEFT JOIN agents a ON a.id = f.uploaded_by
		 WHERE f.team_id = $1 AND f.task_id IS NULL AND f.created_at < $2 AND NOT f.pinned AND f.archived_at IS NULL
		 ORDER BY f.created_at`,
		teamID, olderThan,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkspaceFileRows(rows)
}

func (s *PGTeamStore) CopyFilesToTeam(ctx context.Context, fileIDs []uuid.UUID, targetTeamID uuid.UUID, _, targetChatID, dataDir string) error {
	if len(fileIDs) == 0 {
		return nil
	}

	targetDir := filepath.Join(dataDir, "teams", targetTeamID.String(), targetChatID)
	if err := os.MkdirAll(targetDir, 0750); err != nil {
		return fmt.Errorf("failed to create target workspace dir: %w", err)
	}

	for _, fid := range fileIDs {
		var src store.TeamWorkspaceFileData
		var mimeType sql.NullString
		err := s.db.QueryRowContext(ctx,
			`SELECT id, team_id, channel, chat_id, file_name, mime_type, file_path, size_bytes, uploaded_by, pinned, tags
			 FROM team_workspace_files WHERE id = $1`, fid,
		).Scan(&src.ID, &src.TeamID, &src.Channel, &src.ChatID, &src.FileName, &mimeType, &src.FilePath, &src.SizeBytes, &src.UploadedBy, &src.Pinned, pq.Array(&src.Tags))
		if err != nil {
			continue
		}
		if mimeType.Valid {
			src.MimeType = mimeType.String
		}

		// C1: Guard against path traversal from corrupted DB records.
		if strings.ContainsAny(src.FileName, "/\\") || strings.Contains(src.FileName, "..") || src.FileName == "" {
			continue
		}
		destPath := filepath.Join(targetDir, src.FileName)
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(targetDir)+string(os.PathSeparator)) {
			continue
		}
		if err := copyFile(src.FilePath, destPath); err != nil {
			continue
		}

		// Insert DB record for target team.
		newID := store.GenNewID()
		now := time.Now()
		_, _ = s.db.ExecContext(ctx,
			`INSERT INTO team_workspace_files (id, team_id, channel, chat_id, file_name, mime_type, file_path, size_bytes, uploaded_by, pinned, tags, created_at, updated_at)
			 VALUES ($1, $2, '', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT (team_id, chat_id, file_name) DO UPDATE SET
			   file_path = EXCLUDED.file_path, size_bytes = EXCLUDED.size_bytes, updated_at = EXCLUDED.updated_at`,
			newID, targetTeamID, targetChatID, src.FileName,
			sql.NullString{String: src.MimeType, Valid: src.MimeType != ""},
			destPath, src.SizeBytes, src.UploadedBy, false, pq.Array(src.Tags), now, now,
		)
	}
	return nil
}

func (s *PGTeamStore) GetWorkspaceTotalSize(ctx context.Context, teamID uuid.UUID) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size_bytes), 0) FROM team_workspace_files WHERE team_id = $1`,
		teamID,
	).Scan(&total)
	return total, err
}

// ============================================================
// Workspace file versions
// ============================================================

func (s *PGTeamStore) CreateFileVersion(ctx context.Context, v *store.TeamWorkspaceFileVersionData) error {
	if v.ID == uuid.Nil {
		v.ID = store.GenNewID()
	}
	v.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_workspace_file_versions (id, file_id, version, file_path, size_bytes, uploaded_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		v.ID, v.FileID, v.Version, v.FilePath, v.SizeBytes, v.UploadedBy, v.CreatedAt,
	)
	return err
}

func (s *PGTeamStore) ListFileVersions(ctx context.Context, fileID uuid.UUID) ([]store.TeamWorkspaceFileVersionData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT v.id, v.file_id, v.version, v.file_path, v.size_bytes, v.uploaded_by, v.created_at,
		        COALESCE(a.agent_key, '') AS uploaded_by_key
		 FROM team_workspace_file_versions v
		 LEFT JOIN agents a ON a.id = v.uploaded_by
		 WHERE v.file_id = $1
		 ORDER BY v.version DESC`, fileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []store.TeamWorkspaceFileVersionData
	for rows.Next() {
		var v store.TeamWorkspaceFileVersionData
		if err := rows.Scan(&v.ID, &v.FileID, &v.Version, &v.FilePath, &v.SizeBytes, &v.UploadedBy, &v.CreatedAt, &v.UploadedByKey); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (s *PGTeamStore) GetFileVersion(ctx context.Context, fileID uuid.UUID, version int) (*store.TeamWorkspaceFileVersionData, error) {
	var v store.TeamWorkspaceFileVersionData
	err := s.db.QueryRowContext(ctx,
		`SELECT v.id, v.file_id, v.version, v.file_path, v.size_bytes, v.uploaded_by, v.created_at,
		        COALESCE(a.agent_key, '') AS uploaded_by_key
		 FROM team_workspace_file_versions v
		 LEFT JOIN agents a ON a.id = v.uploaded_by
		 WHERE v.file_id = $1 AND v.version = $2`, fileID, version,
	).Scan(&v.ID, &v.FileID, &v.Version, &v.FilePath, &v.SizeBytes, &v.UploadedBy, &v.CreatedAt, &v.UploadedByKey)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("version %d not found", version)
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *PGTeamStore) PruneOldVersions(ctx context.Context, fileID uuid.UUID, keepN int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`DELETE FROM team_workspace_file_versions
		 WHERE id IN (
		   SELECT id FROM team_workspace_file_versions
		   WHERE file_id = $1
		   ORDER BY version DESC
		   OFFSET $2
		 ) RETURNING file_path`, fileID, keepN,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// ============================================================
// Workspace comments
// ============================================================

func (s *PGTeamStore) AddFileComment(ctx context.Context, c *store.TeamWorkspaceCommentData) error {
	if c.ID == uuid.Nil {
		c.ID = store.GenNewID()
	}
	c.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_workspace_comments (id, file_id, agent_id, content, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		c.ID, c.FileID, c.AgentID, c.Content, c.CreatedAt,
	)
	return err
}

func (s *PGTeamStore) ListFileComments(ctx context.Context, fileID uuid.UUID) ([]store.TeamWorkspaceCommentData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.file_id, c.agent_id, c.content, c.created_at,
		        COALESCE(a.agent_key, '') AS agent_key
		 FROM team_workspace_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.file_id = $1
		 ORDER BY c.created_at`, fileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []store.TeamWorkspaceCommentData
	for rows.Next() {
		var c store.TeamWorkspaceCommentData
		if err := rows.Scan(&c.ID, &c.FileID, &c.AgentID, &c.Content, &c.CreatedAt, &c.AgentKey); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// ============================================================
// Helpers
// ============================================================

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanWorkspaceFile scans a single workspace file row into a struct.
func scanWorkspaceFile(s scanner) (store.TeamWorkspaceFileData, error) {
	var f store.TeamWorkspaceFileData
	var mimeType sql.NullString
	var taskID *uuid.UUID
	var archivedAt *time.Time
	if err := s.Scan(
		&f.ID, &f.TeamID, &f.Channel, &f.ChatID, &f.FileName, &mimeType, &f.FilePath, &f.SizeBytes,
		&f.UploadedBy, &taskID, &f.Pinned, pq.Array(&f.Tags), &f.Metadata, &archivedAt, &f.CreatedAt, &f.UpdatedAt,
		&f.UploadedByKey,
	); err != nil {
		return f, err
	}
	if mimeType.Valid {
		f.MimeType = mimeType.String
	}
	f.TaskID = taskID
	f.ArchivedAt = archivedAt
	return f, nil
}

func scanWorkspaceFileRows(rows *sql.Rows) ([]store.TeamWorkspaceFileData, error) {
	var files []store.TeamWorkspaceFileData
	for rows.Next() {
		f, err := scanWorkspaceFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func copyFile(src, dst string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	df, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer df.Close()

	_, err = io.Copy(df, sf)
	return err
}
