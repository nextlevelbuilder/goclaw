//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func (s *SQLiteSessionStore) Save(ctx context.Context, key string) error {
	s.mu.RLock()
	data, ok := s.cache[sessionCacheKey(ctx, key)]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	snapshot := *data
	msgs := make([]providers.Message, len(data.Messages))
	copy(msgs, data.Messages)
	snapshot.Messages = msgs
	// Deep-copy Metadata under RLock so later mutation does not race with
	// concurrent readers holding data.Metadata via GetSessionMetadata.
	metaCopy := make(map[string]string, len(data.Metadata)+2)
	for k, v := range data.Metadata {
		metaCopy[k] = v
	}
	snapshot.Metadata = metaCopy
	s.mu.RUnlock()

	// Persist adaptive-throttle numbers into metadata JSON so list queries can
	// read accurate token counts without a dedicated column.
	if snapshot.LastPromptTokens > 0 {
		snapshot.Metadata["last_prompt_tokens"] = strconv.Itoa(snapshot.LastPromptTokens)
		snapshot.Metadata["last_message_count"] = strconv.Itoa(snapshot.LastMessageCount)
	}

	msgsJSON, _ := json.Marshal(snapshot.Messages)
	metaJSON := []byte("{}")
	if len(snapshot.Metadata) > 0 {
		metaJSON, _ = json.Marshal(snapshot.Metadata)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET
			messages = ?, summary = ?, model = ?, provider = ?, channel = ?,
			input_tokens = ?, output_tokens = ?, compaction_count = ?,
			memory_flush_compaction_count = ?, memory_flush_at = ?,
			label = ?, spawned_by = ?, spawn_depth = ?,
			agent_id = ?, user_id = ?, metadata = ?, updated_at = ?,
			team_id = ?
		 WHERE session_key = ? AND tenant_id = ?`,
		msgsJSON, nilStr(snapshot.Summary), nilStr(snapshot.Model), nilStr(snapshot.Provider), nilStr(snapshot.Channel),
		snapshot.InputTokens, snapshot.OutputTokens, snapshot.CompactionCount,
		snapshot.MemoryFlushCompactionCount, snapshot.MemoryFlushAt,
		nilStr(snapshot.Label), nilStr(snapshot.SpawnedBy), snapshot.SpawnDepth,
		nilSessionUUID(snapshot.AgentUUID), nilStr(snapshot.UserID), metaJSON, snapshot.Updated,
		snapshot.TeamID,
		key, tenantIDForInsert(ctx),
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Session not yet in DB (e.g. cron/heartbeat sessions) — insert it.
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO sessions (id, session_key, messages, summary, model, provider, channel,
				input_tokens, output_tokens, compaction_count,
				memory_flush_compaction_count, memory_flush_at,
				label, spawned_by, spawn_depth, agent_id, user_id, metadata, updated_at, team_id, tenant_id, created_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(session_key, tenant_id) DO UPDATE SET
				messages = excluded.messages, summary = excluded.summary, model = excluded.model,
				provider = excluded.provider, channel = excluded.channel,
				input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens,
				compaction_count = excluded.compaction_count,
				memory_flush_compaction_count = excluded.memory_flush_compaction_count,
				memory_flush_at = excluded.memory_flush_at,
				label = excluded.label, spawned_by = excluded.spawned_by, spawn_depth = excluded.spawn_depth,
				agent_id = excluded.agent_id, user_id = excluded.user_id, metadata = excluded.metadata,
				updated_at = excluded.updated_at, team_id = excluded.team_id`,
			uuid.Must(uuid.NewV7()), key, msgsJSON,
			nilStr(snapshot.Summary), nilStr(snapshot.Model), nilStr(snapshot.Provider), nilStr(snapshot.Channel),
			snapshot.InputTokens, snapshot.OutputTokens, snapshot.CompactionCount,
			snapshot.MemoryFlushCompactionCount, snapshot.MemoryFlushAt,
			nilStr(snapshot.Label), nilStr(snapshot.SpawnedBy), snapshot.SpawnDepth,
			nilSessionUUID(snapshot.AgentUUID), nilStr(snapshot.UserID), metaJSON, snapshot.Updated,
			snapshot.TeamID, tenantIDForInsert(ctx), snapshot.Updated,
		)
		return err
	}
	return nil
}

func (s *SQLiteSessionStore) TruncateHistory(ctx context.Context, key string, keepLast int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		if keepLast <= 0 {
			data.Messages = []providers.Message{}
		} else if len(data.Messages) > keepLast {
			data.Messages = data.Messages[len(data.Messages)-keepLast:]
		}
		data.Updated = time.Now()
	}
}

func (s *SQLiteSessionStore) SetHistory(ctx context.Context, key string, msgs []providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Messages = msgs
		data.Updated = time.Now()
	}
}

func (s *SQLiteSessionStore) Reset(ctx context.Context, key string) {
	s.mu.Lock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Messages = []providers.Message{}
		data.Summary = ""
		data.Updated = time.Now()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Session not in cache — clear directly in DB.
	tid := tenantIDForInsert(ctx)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET messages = '[]', summary = '', updated_at = ?
		 WHERE session_key = ? AND tenant_id = ?`,
		time.Now(), key, tid,
	); err != nil {
		slog.Warn("sessions.reset_db_fallback_failed", "key", key, "error", err)
	}
}

func (s *SQLiteSessionStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.cache, sessionCacheKey(ctx, key))
	s.mu.Unlock()

	if s.OnDelete != nil {
		s.OnDelete(key)
	}

	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE session_key = ? AND tenant_id = ?", key, tid)
	return err
}

func (s *SQLiteSessionStore) LastUsedChannel(ctx context.Context, agentID string) (string, string) {
	prefix := "agent:" + agentID + ":%"
	tid := tenantIDForInsert(ctx)
	var sessionKey string
	err := s.db.QueryRowContext(ctx,
		`SELECT session_key FROM sessions
		 WHERE session_key LIKE ?
		   AND session_key NOT LIKE ?
		   AND session_key NOT LIKE ?
		   AND tenant_id = ?
		 ORDER BY updated_at DESC LIMIT 1`,
		prefix,
		"agent:"+agentID+":cron:%",
		"agent:"+agentID+":subagent:%",
		tid,
	).Scan(&sessionKey)
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(sessionKey, ":", 5)
	if len(parts) >= 5 {
		return parts[2], parts[4]
	}
	return "", ""
}
