//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- Agent-level Context Files ---

func (s *SQLiteAgentStore) GetAgentContextFiles(ctx context.Context, agentID uuid.UUID) ([]store.AgentContextFileData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT agent_id, file_name, content FROM agent_context_files WHERE agent_id = ?"+tClause+" ORDER BY file_name",
		append([]any{agentID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.AgentContextFileData
	for rows.Next() {
		var d store.AgentContextFileData
		if err := rows.Scan(&d.AgentID, &d.FileName, &d.Content); err != nil {
			continue
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *SQLiteAgentStore) SetAgentContextFile(ctx context.Context, agentID uuid.UUID, fileName, content string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_context_files (id, agent_id, file_name, content, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (agent_id, file_name) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		store.GenNewID(), agentID, fileName, content, time.Now(), tenantIDForInsert(ctx),
	)
	return err
}

// PropagateContextFile copies an agent-level context file to all existing user
// instances that already have that file. Returns updated row count.
// SQLite does not support UPDATE...FROM, so we use a 2-step approach.
func (s *SQLiteAgentStore) PropagateContextFile(ctx context.Context, agentID uuid.UUID, fileName string) (int, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}

	// Step 1: fetch the agent-level content.
	var content string
	err = s.db.QueryRowContext(ctx,
		"SELECT content FROM agent_context_files WHERE agent_id = ? AND file_name = ?"+tClause,
		append([]any{agentID, fileName}, tArgs...)...,
	).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// Step 2: update all matching user context files.
	res, err := s.db.ExecContext(ctx,
		"UPDATE user_context_files SET content = ?, updated_at = ? WHERE agent_id = ? AND file_name = ?"+tClause,
		append([]any{content, time.Now(), agentID, fileName}, tArgs...)...,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// --- Per-user Context Files ---

func (s *SQLiteAgentStore) GetUserContextFiles(ctx context.Context, agentID uuid.UUID, userID string) ([]store.UserContextFileData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT agent_id, user_id, file_name, content FROM user_context_files WHERE agent_id = ? AND user_id = ?"+tClause+" ORDER BY file_name",
		append([]any{agentID, userID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.UserContextFileData
	for rows.Next() {
		var d store.UserContextFileData
		if err := rows.Scan(&d.AgentID, &d.UserID, &d.FileName, &d.Content); err != nil {
			continue
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *SQLiteAgentStore) SetUserContextFile(ctx context.Context, agentID uuid.UUID, userID, fileName, content string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_context_files (id, agent_id, user_id, file_name, content, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (agent_id, user_id, file_name) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		store.GenNewID(), agentID, userID, fileName, content, time.Now(), tenantIDForInsert(ctx),
	)
	return err
}

func (s *SQLiteAgentStore) ListUserContextFilesByName(ctx context.Context, agentID uuid.UUID, fileName string) ([]store.UserContextFileData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT agent_id, user_id, file_name, content FROM user_context_files WHERE agent_id = ? AND file_name = ?"+tClause,
		append([]any{agentID, fileName}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.UserContextFileData
	for rows.Next() {
		var d store.UserContextFileData
		if err := rows.Scan(&d.AgentID, &d.UserID, &d.FileName, &d.Content); err != nil {
			continue
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *SQLiteAgentStore) DeleteUserContextFile(ctx context.Context, agentID uuid.UUID, userID, fileName string) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM user_context_files WHERE agent_id = ? AND user_id = ? AND file_name = ?"+tClause,
		append([]any{agentID, userID, fileName}, tArgs...)...)
	return err
}

func (s *SQLiteAgentStore) MigrateUserDataOnMerge(ctx context.Context, oldUserIDs []string, newUserID string) error {
	if len(oldUserIDs) == 0 {
		return nil
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}

	placeholders := make([]string, len(oldUserIDs))
	baseArgs := make([]any, 0, len(oldUserIDs)+1+len(tArgs))
	for i, id := range oldUserIDs {
		placeholders[i] = "?"
		baseArgs = append(baseArgs, id)
	}
	inClause := strings.Join(placeholders, ",")
	baseArgs = append(baseArgs, newUserID)
	baseArgs = append(baseArgs, tArgs...)

	delArgs := make([]any, len(oldUserIDs))
	copy(delArgs, baseArgs[:len(oldUserIDs)])
	delArgs = append(delArgs, tArgs...)

	uuid := `lower(hex(randomblob(4))||'-'||hex(randomblob(2))||'-'||hex(randomblob(2))||'-'||hex(randomblob(2))||'-'||hex(randomblob(6)))`

	// DO NOTHING on conflict — existing tenant user data always wins.
	migrate := func(insertQ, deleteQ string) {
		if _, err := s.db.ExecContext(ctx, insertQ, baseArgs...); err != nil {
			slog.Warn("merge.migrate", "error", err)
		}
		if _, err := s.db.ExecContext(ctx, deleteQ, delArgs...); err != nil {
			slog.Warn("merge.cleanup", "error", err)
		}
	}

	// 1. user_context_files
	migrate(
		fmt.Sprintf(`INSERT INTO user_context_files (id, agent_id, user_id, file_name, content, updated_at, tenant_id)
			SELECT %s, agent_id, ?, file_name, content, updated_at, tenant_id
			FROM user_context_files WHERE user_id IN (%s)%s
			ON CONFLICT (agent_id, user_id, file_name) DO NOTHING`, uuid, inClause, tClause),
		fmt.Sprintf(`DELETE FROM user_context_files WHERE user_id IN (%s)%s`, inClause, tClause),
	)

	// 2. user_agent_overrides
	migrate(
		fmt.Sprintf(`INSERT INTO user_agent_overrides (id, agent_id, user_id, provider, model, settings, created_at, updated_at, tenant_id)
			SELECT %s, agent_id, ?, provider, model, settings, created_at, updated_at, tenant_id
			FROM user_agent_overrides WHERE user_id IN (%s)%s
			ON CONFLICT (agent_id, user_id) DO NOTHING`, uuid, inClause, tClause),
		fmt.Sprintf(`DELETE FROM user_agent_overrides WHERE user_id IN (%s)%s`, inClause, tClause),
	)

	// 3. user_agent_profiles
	migrate(
		fmt.Sprintf(`INSERT INTO user_agent_profiles (agent_id, user_id, workspace, first_seen_at, last_seen_at, metadata, tenant_id)
			SELECT agent_id, ?, workspace, first_seen_at, last_seen_at, metadata, tenant_id
			FROM user_agent_profiles WHERE user_id IN (%s)%s
			ON CONFLICT (agent_id, user_id) DO NOTHING`, inClause, tClause),
		fmt.Sprintf(`DELETE FROM user_agent_profiles WHERE user_id IN (%s)%s`, inClause, tClause),
	)

	// 4. memory_documents
	migrate(
		fmt.Sprintf(`INSERT INTO memory_documents (id, agent_id, user_id, path, content, hash, updated_at, created_at, tenant_id)
			SELECT %s, agent_id, ?, path, content, hash, updated_at, created_at, tenant_id
			FROM memory_documents WHERE user_id IN (%s)%s
			ON CONFLICT (agent_id, COALESCE(user_id,''), path) DO NOTHING`, uuid, inClause, tClause),
		fmt.Sprintf(`DELETE FROM memory_documents WHERE user_id IN (%s)%s`, inClause, tClause),
	)

	// 5. memory_chunks: re-point remaining chunks.
	repoint := fmt.Sprintf(`UPDATE memory_chunks SET user_id = ? WHERE user_id IN (%s)%s`, inClause, tClause)
	if _, err := s.db.ExecContext(ctx, repoint, baseArgs...); err != nil {
		slog.Warn("merge.migrate_chunks", "error", err)
	}

	return nil
}

// --- User-Agent Profiles ---

// GetOrCreateUserProfile upserts a user profile and returns (isNew, effectiveWorkspace, error).
// SQLite has no xmax trick; we use INSERT OR IGNORE + UPDATE + SELECT to detect insert vs update.
func (s *SQLiteAgentStore) GetOrCreateUserProfile(ctx context.Context, agentID uuid.UUID, userID, workspace, channel string) (bool, string, error) {
	effectiveWs := config.ContractHome(workspace)
	if channel != "" {
		effectiveWs = filepath.Join(effectiveWs, channel)
	}

	tid := tenantIDForInsert(ctx)
	now := time.Now()

	// Try insert (ignored on conflict). Use changes() to detect if row was actually inserted.
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO user_agent_profiles (agent_id, user_id, workspace, first_seen_at, last_seen_at, tenant_id)
		 VALUES (?, ?, NULLIF(?, ''), ?, ?, ?)`,
		agentID, userID, effectiveWs, now, now, tid,
	)
	if err != nil {
		return false, effectiveWs, err
	}
	inserted, _ := res.RowsAffected()
	isNew := inserted > 0

	// Always bump last_seen_at for existing rows.
	if !isNew {
		_, err = s.db.ExecContext(ctx,
			`UPDATE user_agent_profiles SET last_seen_at = ? WHERE agent_id = ? AND user_id = ?`,
			now, agentID, userID,
		)
		if err != nil {
			return false, effectiveWs, err
		}
	}

	// Read back stored workspace.
	var storedWorkspace sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT workspace FROM user_agent_profiles WHERE agent_id = ? AND user_id = ?`,
		agentID, userID,
	).Scan(&storedWorkspace)
	if err != nil {
		return false, effectiveWs, err
	}

	ws := effectiveWs
	if storedWorkspace.Valid && storedWorkspace.String != "" {
		ws = storedWorkspace.String
	}
	return isNew, ws, nil
}

// EnsureUserProfile creates a minimal user_agent_profiles row if not exists.
func (s *SQLiteAgentStore) EnsureUserProfile(ctx context.Context, agentID uuid.UUID, userID string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO user_agent_profiles (agent_id, user_id, first_seen_at, last_seen_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?)`,
		agentID, userID, now, now, tenantIDForInsert(ctx),
	)
	return err
}

// --- User Instances ---

func (s *SQLiteAgentStore) ListUserInstances(ctx context.Context, agentID uuid.UUID) ([]store.UserInstanceData, error) {
	tClause, tArgs, err := scopeClauseAlias(ctx, "p")
	if err != nil {
		return nil, err
	}
	subTenantFilter := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			subTenantFilter = " AND tenant_id = ?"
			// tArgs already has tid; reuse it by appending another copy below.
		}
	}
	// Build args: subquery agent_id [+ optional tenant_id], main WHERE agent_id, scope args.
	var queryArgs []any
	queryArgs = append(queryArgs, agentID) // subquery: agent_id = ?
	if subTenantFilter != "" {
		queryArgs = append(queryArgs, tArgs[0]) // subquery: tenant_id = ?
	}
	queryArgs = append(queryArgs, agentID) // main WHERE: p.agent_id = ?
	queryArgs = append(queryArgs, tArgs...) // scope clause args
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.user_id,
		       strftime('%Y-%m-%dT%H:%M:%SZ', p.first_seen_at) AS first_seen_at,
		       strftime('%Y-%m-%dT%H:%M:%SZ', p.last_seen_at) AS last_seen_at,
		       COALESCE(fc.cnt, 0) AS file_count,
		       COALESCE(p.metadata, '{}')
		FROM user_agent_profiles p
		LEFT JOIN (
		    SELECT user_id, COUNT(*) AS cnt
		    FROM user_context_files
		    WHERE agent_id = ?`+subTenantFilter+`
		    GROUP BY user_id
		) fc ON fc.user_id = p.user_id
		WHERE p.agent_id = ?`+tClause+`
		ORDER BY p.last_seen_at DESC
	`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.UserInstanceData
	for rows.Next() {
		var d store.UserInstanceData
		var metaJSON []byte
		if err := rows.Scan(&d.UserID, &d.FirstSeenAt, &d.LastSeenAt, &d.FileCount, &metaJSON); err != nil {
			continue
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &d.Metadata)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *SQLiteAgentStore) UpdateUserProfileMetadata(ctx context.Context, agentID uuid.UUID, userID string, metadata map[string]string) error {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE user_agent_profiles SET metadata = json_patch(COALESCE(metadata, '{}'), ?)
		 WHERE agent_id = ? AND user_id = ?`+tClause,
		append([]any{metaJSON, agentID, userID}, tArgs...)...,
	)
	return err
}

// --- User Overrides ---

func (s *SQLiteAgentStore) GetUserOverride(ctx context.Context, agentID uuid.UUID, userID string) (*store.UserAgentOverrideData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	var d store.UserAgentOverrideData
	err = s.db.QueryRowContext(ctx,
		"SELECT agent_id, user_id, provider, model FROM user_agent_overrides WHERE agent_id = ? AND user_id = ?"+tClause,
		append([]any{agentID, userID}, tArgs...)...,
	).Scan(&d.AgentID, &d.UserID, &d.Provider, &d.Model)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, nil
	}
	return &d, nil
}

func (s *SQLiteAgentStore) SetUserOverride(ctx context.Context, override *store.UserAgentOverrideData) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_agent_overrides (id, agent_id, user_id, provider, model, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (agent_id, user_id) DO UPDATE SET provider = excluded.provider, model = excluded.model`,
		store.GenNewID(), override.AgentID, override.UserID, override.Provider, override.Model, tenantIDForInsert(ctx),
	)
	return err
}
