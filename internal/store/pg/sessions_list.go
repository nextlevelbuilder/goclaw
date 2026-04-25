package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// buildSessionFilter builds a dynamic WHERE clause from SessionListOpts.
// Returns the WHERE string (with leading " WHERE ") and the positional args.
// The tableAlias is prepended to column names (e.g. "s" → "s.session_key").
//
// Tenant isolation precedence:
//  1. opts.TenantID (if set) wins — admin tooling override path.
//  2. Else if ctx is NOT cross-tenant, fall back to store.TenantIDFromContext(ctx).
//     This matches the canonical pattern in List() above and prevents
//     silent cross-tenant reads from callers that rely on ctx scoping.
//  3. Else (cross-tenant ctx, no opts override) no tenant filter.
func buildSessionFilter(ctx context.Context, opts store.SessionListOpts, tableAlias string) (string, []any) {
	prefix := ""
	if tableAlias != "" {
		prefix = tableAlias + "."
	}
	var conditions []string
	var args []any
	idx := 1

	if opts.AgentID != "" {
		conditions = append(conditions, fmt.Sprintf("%ssession_key LIKE $%d", prefix, idx))
		args = append(args, "agent:"+opts.AgentID+":%")
		idx++
	}
	if opts.Channel != "" {
		// Match canonical format: agent:X:{channel}:...
		conditions = append(conditions, fmt.Sprintf("%ssession_key LIKE $%d", prefix, idx))
		args = append(args, "agent:%:"+opts.Channel+":%")
		idx++
	}
	if opts.UserID != "" {
		conditions = append(conditions, fmt.Sprintf("%suser_id = $%d", prefix, idx))
		args = append(args, opts.UserID)
		idx++
	}

	// Resolve tenant filter — opts override beats ctx.
	tenantID := opts.TenantID
	if tenantID == uuid.Nil && !store.IsCrossTenant(ctx) {
		tenantID = store.TenantIDFromContext(ctx)
	}
	if tenantID != uuid.Nil {
		conditions = append(conditions, fmt.Sprintf("%stenant_id = $%d", prefix, idx))
		args = append(args, tenantID)
		idx++
	}
	_ = idx // consumed

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *PGSessionStore) List(ctx context.Context, agentID string) []store.SessionInfo {
	var conditions []string
	var args []any
	idx := 1

	if agentID != "" {
		conditions = append(conditions, fmt.Sprintf("session_key LIKE $%d", idx))
		args = append(args, "agent:"+agentID+":%")
		idx++
	}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", idx))
			args = append(args, tid)
			idx++
		}
	}
	_ = idx

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var scanned []sessionListRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned,
		"SELECT session_key, messages, created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}') AS metadata FROM sessions"+where+" ORDER BY updated_at DESC",
		args...); err != nil {
		return nil
	}

	var result []store.SessionInfo
	for i := range scanned {
		var msgs []providers.Message
		json.Unmarshal(scanned[i].MsgsJSON, &msgs) //nolint:errcheck
		result = append(result, scanned[i].toSessionInfo(len(msgs)))
	}
	return result
}

func (s *PGSessionStore) ListPaged(ctx context.Context, opts store.SessionListOpts) store.SessionListResult {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := max(opts.Offset, 0)

	where, whereArgs := buildSessionFilter(ctx, opts, "")

	// Count total
	var total int
	countQ := "SELECT COUNT(*) FROM sessions" + where
	if err := s.db.QueryRowContext(ctx, countQ, whereArgs...).Scan(&total); err != nil {
		return store.SessionListResult{Sessions: []store.SessionInfo{}, Total: 0}
	}

	// Fetch page using jsonb_array_length to avoid loading full messages
	nextIdx := len(whereArgs) + 1
	selectQ := fmt.Sprintf(`SELECT session_key, jsonb_array_length(messages) AS message_count, created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}') AS metadata
		FROM sessions%s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, nextIdx, nextIdx+1)
	selectArgs := append(append([]any{}, whereArgs...), limit, offset)

	var scanned []sessionPagedRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, selectQ, selectArgs...); err != nil {
		return store.SessionListResult{Sessions: []store.SessionInfo{}, Total: total}
	}

	result := make([]store.SessionInfo, 0, len(scanned))
	for i := range scanned {
		result = append(result, scanned[i].toSessionInfo())
	}
	return store.SessionListResult{Sessions: result, Total: total}
}

// ListPagedRich returns enriched session info for API responses (includes model, tokens, agent name).
func (s *PGSessionStore) ListPagedRich(ctx context.Context, opts store.SessionListOpts) store.SessionListRichResult {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := max(opts.Offset, 0)

	where, whereArgs := buildSessionFilter(ctx, opts, "s")

	// Count total
	var total int
	countQ := "SELECT COUNT(*) FROM sessions s" + where
	if err := s.db.QueryRowContext(ctx, countQ, whereArgs...).Scan(&total); err != nil {
		return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}, Total: 0}
	}

	// Fetch page with agent name via LEFT JOIN
	const richCols = `s.session_key, jsonb_array_length(s.messages) AS message_count, s.created_at, s.updated_at,
		s.label, s.channel, s.user_id, COALESCE(s.metadata, '{}') AS metadata,
		s.model, s.provider, s.input_tokens, s.output_tokens,
		COALESCE(a.display_name, '') AS agent_name,
		COALESCE(
		  NULLIF(s.metadata->>'last_prompt_tokens', '')::int,
		  octet_length(s.messages::text) / 4 + 12000
		) AS estimated_tokens,
		COALESCE(a.context_window, 200000) AS context_window,
		s.compaction_count`

	nextIdx := len(whereArgs) + 1
	selectQ := fmt.Sprintf(`SELECT %s
		FROM sessions s LEFT JOIN agents a ON s.agent_id = a.id
		%s ORDER BY s.updated_at DESC LIMIT $%d OFFSET $%d`, richCols, where, nextIdx, nextIdx+1)
	selectArgs := append(append([]any{}, whereArgs...), limit, offset)

	var scanned []sessionRichRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, selectQ, selectArgs...); err != nil {
		return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}, Total: total}
	}

	result := make([]store.SessionInfoRich, 0, len(scanned))
	for i := range scanned {
		result = append(result, scanned[i].toSessionInfoRich())
	}
	return store.SessionListRichResult{Sessions: result, Total: total}
}

func (s *PGSessionStore) Save(ctx context.Context, key string) error {
	s.mu.RLock()
	data, ok := s.cache[sessionCacheKey(ctx, key)]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	// Snapshot
	snapshot := *data
	msgs := make([]providers.Message, len(data.Messages))
	copy(msgs, data.Messages)
	snapshot.Messages = msgs
	// Deep-copy Metadata under RLock so subsequent mutation does not race with
	// concurrent readers holding data.Metadata via GetSessionMetadata.
	metaCopy := make(map[string]string, len(data.Metadata)+2)
	for k, v := range data.Metadata {
		metaCopy[k] = v
	}
	snapshot.Metadata = metaCopy
	s.mu.RUnlock()

	// Persist adaptive-throttle numbers into metadata JSONB so list queries can
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
			messages = $1, summary = $2, model = $3, provider = $4, channel = $5,
			input_tokens = $6, output_tokens = $7, compaction_count = $8,
			memory_flush_compaction_count = $9, memory_flush_at = $10,
			label = $11, spawned_by = $12, spawn_depth = $13,
			agent_id = $14, user_id = $15, metadata = $16, updated_at = $17,
			team_id = $18
		 WHERE session_key = $19 AND tenant_id = $20`,
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
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
			 ON CONFLICT (tenant_id, session_key) DO UPDATE SET
				messages = EXCLUDED.messages, summary = EXCLUDED.summary, model = EXCLUDED.model,
				provider = EXCLUDED.provider, channel = EXCLUDED.channel,
				input_tokens = EXCLUDED.input_tokens, output_tokens = EXCLUDED.output_tokens,
				compaction_count = EXCLUDED.compaction_count,
				memory_flush_compaction_count = EXCLUDED.memory_flush_compaction_count,
				memory_flush_at = EXCLUDED.memory_flush_at,
				label = EXCLUDED.label, spawned_by = EXCLUDED.spawned_by, spawn_depth = EXCLUDED.spawn_depth,
				agent_id = EXCLUDED.agent_id, user_id = EXCLUDED.user_id, metadata = EXCLUDED.metadata,
				updated_at = EXCLUDED.updated_at, team_id = EXCLUDED.team_id`,
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

func (s *PGSessionStore) LastUsedChannel(ctx context.Context, agentID string) (string, string) {
	prefix := "agent:" + agentID + ":%"
	tid := tenantIDForInsert(ctx)
	var sessionKey string
	err := s.db.QueryRowContext(ctx,
		`SELECT session_key FROM sessions
		 WHERE session_key LIKE $1
		   AND session_key NOT LIKE $2
		   AND session_key NOT LIKE $3
		   AND tenant_id = $4
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

// --- helpers ---

func (s *PGSessionStore) getOrInit(ctx context.Context, key string) *store.SessionData {
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return data
	}

	// Try loading from DB first to avoid overwriting existing messages
	data := s.loadFromDB(ctx, key)
	if data != nil {
		s.cache[sessionCacheKey(ctx, key)] = data
		return data
	}

	// Not in DB — create new
	now := time.Now()
	data = &store.SessionData{
		Key:      key,
		Messages: []providers.Message{},
		Created:  now,
		Updated:  now,
	}
	s.cache[sessionCacheKey(ctx, key)] = data

	msgsJSON, _ := json.Marshal([]providers.Message{})
	s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, session_key, messages, created_at, updated_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tenant_id, session_key) DO NOTHING`,
		uuid.Must(uuid.NewV7()), key, msgsJSON, now, now, tenantIDForInsert(ctx),
	)
	return data
}

func (s *PGSessionStore) loadFromDB(ctx context.Context, key string) *store.SessionData {
	var sessionKey string
	var msgsJSON []byte
	var summary, model, provider, channel, label, spawnedBy, userID *string
	var agentID, teamID *uuid.UUID
	var inputTokens, outputTokens int64
	var compactionCount, memoryFlushCompactionCount, spawnDepth int
	var memoryFlushAt int64
	var createdAt, updatedAt time.Time
	var metaJSON *[]byte

	tid := tenantIDForInsert(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT session_key, messages, summary, model, provider, channel,
		 input_tokens, output_tokens, compaction_count,
		 memory_flush_compaction_count, memory_flush_at,
		 label, spawned_by, spawn_depth, agent_id, user_id,
		 COALESCE(metadata, '{}'), created_at, updated_at, team_id
		 FROM sessions WHERE session_key = $1 AND tenant_id = $2`, key, tid,
	).Scan(&sessionKey, &msgsJSON, &summary, &model, &provider, &channel,
		&inputTokens, &outputTokens, &compactionCount,
		&memoryFlushCompactionCount, &memoryFlushAt,
		&label, &spawnedBy, &spawnDepth, &agentID, &userID,
		&metaJSON, &createdAt, &updatedAt, &teamID)
	if err != nil {
		return nil
	}

	var msgs []providers.Message
	json.Unmarshal(msgsJSON, &msgs)

	var meta map[string]string
	if metaJSON != nil {
		json.Unmarshal(*metaJSON, &meta)
	}

	// Restore adaptive-throttle fields from metadata so GetLastPromptTokens()
	// returns the persisted value after a server restart (clean cache).
	var lastPromptTokens, lastMessageCount int
	if meta != nil {
		if v := meta["last_prompt_tokens"]; v != "" {
			lastPromptTokens, _ = strconv.Atoi(v)
		}
		if v := meta["last_message_count"]; v != "" {
			lastMessageCount, _ = strconv.Atoi(v)
		}
	}

	return &store.SessionData{
		Key:                        sessionKey,
		Messages:                   msgs,
		Summary:                    derefStr(summary),
		Created:                    createdAt,
		Updated:                    updatedAt,
		AgentUUID:                  derefUUID(agentID),
		UserID:                     derefStr(userID),
		TeamID:                     teamID,
		Model:                      derefStr(model),
		Provider:                   derefStr(provider),
		Channel:                    derefStr(channel),
		InputTokens:                inputTokens,
		OutputTokens:               outputTokens,
		CompactionCount:            compactionCount,
		MemoryFlushCompactionCount: memoryFlushCompactionCount,
		MemoryFlushAt:              memoryFlushAt,
		Label:                      derefStr(label),
		SpawnedBy:                  derefStr(spawnedBy),
		SpawnDepth:                 spawnDepth,
		Metadata:                   meta,
		LastPromptTokens:           lastPromptTokens,
		LastMessageCount:           lastMessageCount,
	}
}

func nilSessionUUID(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}
