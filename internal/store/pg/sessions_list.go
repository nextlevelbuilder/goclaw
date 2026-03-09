package pg

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGSessionStore) List(agentID string) []store.SessionInfo {
	var rows *sql.Rows
	var err error
	if agentID != "" {
		prefix := "agent:" + agentID + ":%"
		rows, err = s.db.Query(
			"SELECT session_key, messages, created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}') FROM sessions WHERE session_key LIKE $1 ORDER BY updated_at DESC", prefix)
	} else {
		rows, err = s.db.Query(
			"SELECT session_key, messages, created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}') FROM sessions ORDER BY updated_at DESC")
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.SessionInfo
	for rows.Next() {
		var key string
		var msgsJSON []byte
		var createdAt, updatedAt time.Time
		var label, channel, userID *string
		var metaJSON []byte
		if err := rows.Scan(&key, &msgsJSON, &createdAt, &updatedAt, &label, &channel, &userID, &metaJSON); err != nil {
			continue
		}
		var msgs []providers.Message
		json.Unmarshal(msgsJSON, &msgs)
		var meta map[string]string
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &meta)
		}
		result = append(result, store.SessionInfo{
			Key:          key,
			MessageCount: len(msgs),
			Created:      createdAt,
			Updated:      updatedAt,
			Label:        derefStr(label),
			Channel:      derefStr(channel),
			UserID:       derefStr(userID),
			Metadata:     meta,
		})
	}
	return result
}

func (s *PGSessionStore) ListPaged(opts store.SessionListOpts) store.SessionListResult {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	var where string
	var whereArgs []interface{}

	if opts.AgentID != "" {
		where = " WHERE session_key LIKE $1"
		whereArgs = append(whereArgs, "agent:"+opts.AgentID+":%")
	}

	// Count total
	var total int
	countQ := "SELECT COUNT(*) FROM sessions" + where
	if err := s.db.QueryRow(countQ, whereArgs...).Scan(&total); err != nil {
		return store.SessionListResult{Sessions: []store.SessionInfo{}, Total: 0}
	}

	// Fetch page using jsonb_array_length to avoid loading full messages
	var selectQ string
	var selectArgs []interface{}

	if opts.AgentID != "" {
		selectQ = `SELECT session_key, jsonb_array_length(messages), created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}')
		           FROM sessions WHERE session_key LIKE $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`
		selectArgs = []interface{}{whereArgs[0], limit, offset}
	} else {
		selectQ = `SELECT session_key, jsonb_array_length(messages), created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}')
		           FROM sessions ORDER BY updated_at DESC LIMIT $1 OFFSET $2`
		selectArgs = []interface{}{limit, offset}
	}

	rows, err := s.db.Query(selectQ, selectArgs...)
	if err != nil {
		return store.SessionListResult{Sessions: []store.SessionInfo{}, Total: total}
	}
	defer rows.Close()

	var result []store.SessionInfo
	for rows.Next() {
		var key string
		var msgCount int
		var createdAt, updatedAt time.Time
		var label, channel, userID *string
		var metaJSON []byte
		if err := rows.Scan(&key, &msgCount, &createdAt, &updatedAt, &label, &channel, &userID, &metaJSON); err != nil {
			continue
		}
		var meta map[string]string
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &meta)
		}
		result = append(result, store.SessionInfo{
			Key:          key,
			MessageCount: msgCount,
			Created:      createdAt,
			Updated:      updatedAt,
			Label:        derefStr(label),
			Channel:      derefStr(channel),
			UserID:       derefStr(userID),
			Metadata:     meta,
		})
	}
	if result == nil {
		result = []store.SessionInfo{}
	}
	return store.SessionListResult{Sessions: result, Total: total}
}

// ListPagedRich returns enriched session info for API responses (includes model, tokens, agent name).
func (s *PGSessionStore) ListPagedRich(opts store.SessionListOpts) store.SessionListRichResult {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	var where string
	var whereArgs []interface{}

	if opts.AgentID != "" {
		where = " WHERE s.session_key LIKE $1"
		whereArgs = append(whereArgs, "agent:"+opts.AgentID+":%")
	}

	// Count total
	var total int
	countQ := "SELECT COUNT(*) FROM sessions s" + where
	if err := s.db.QueryRow(countQ, whereArgs...).Scan(&total); err != nil {
		return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}, Total: 0}
	}

	// Fetch page with agent name via LEFT JOIN
	var selectQ string
	var selectArgs []interface{}

	const richCols = `s.session_key, jsonb_array_length(s.messages), s.created_at, s.updated_at,
		s.label, s.channel, s.user_id, COALESCE(s.metadata, '{}'),
		s.model, s.provider, s.input_tokens, s.output_tokens,
		COALESCE(a.display_name, '')`

	if opts.AgentID != "" {
		selectQ = `SELECT ` + richCols + `
		           FROM sessions s LEFT JOIN agents a ON s.agent_id = a.id
		           WHERE s.session_key LIKE $1 ORDER BY s.updated_at DESC LIMIT $2 OFFSET $3`
		selectArgs = []interface{}{whereArgs[0], limit, offset}
	} else {
		selectQ = `SELECT ` + richCols + `
		           FROM sessions s LEFT JOIN agents a ON s.agent_id = a.id
		           ORDER BY s.updated_at DESC LIMIT $1 OFFSET $2`
		selectArgs = []interface{}{limit, offset}
	}

	rows, err := s.db.Query(selectQ, selectArgs...)
	if err != nil {
		return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}, Total: total}
	}
	defer rows.Close()

	var result []store.SessionInfoRich
	for rows.Next() {
		var key string
		var msgCount int
		var createdAt, updatedAt time.Time
		var label, channel, userID *string
		var metaJSON []byte
		var model, provider *string
		var inputTokens, outputTokens int64
		var agentName string
		if err := rows.Scan(&key, &msgCount, &createdAt, &updatedAt, &label, &channel, &userID, &metaJSON,
			&model, &provider, &inputTokens, &outputTokens, &agentName); err != nil {
			continue
		}
		var meta map[string]string
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &meta)
		}
		result = append(result, store.SessionInfoRich{
			SessionInfo: store.SessionInfo{
				Key:          key,
				MessageCount: msgCount,
				Created:      createdAt,
				Updated:      updatedAt,
				Label:        derefStr(label),
				Channel:      derefStr(channel),
				UserID:       derefStr(userID),
				Metadata:     meta,
			},
			Model:        derefStr(model),
			Provider:     derefStr(provider),
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			AgentName:    agentName,
		})
	}
	if result == nil {
		result = []store.SessionInfoRich{}
	}
	return store.SessionListRichResult{Sessions: result, Total: total}
}

func (s *PGSessionStore) Save(key string) error {
	s.mu.RLock()
	data, ok := s.cache[key]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	// Snapshot
	snapshot := *data
	msgs := make([]providers.Message, len(data.Messages))
	copy(msgs, data.Messages)
	snapshot.Messages = msgs
	s.mu.RUnlock()

	msgsJSON, _ := json.Marshal(snapshot.Messages)
	metaJSON := []byte("{}")
	if len(snapshot.Metadata) > 0 {
		metaJSON, _ = json.Marshal(snapshot.Metadata)
	}

	_, err := s.db.Exec(
		`UPDATE sessions SET
			messages = $1, summary = $2, model = $3, provider = $4, channel = $5,
			input_tokens = $6, output_tokens = $7, compaction_count = $8,
			memory_flush_compaction_count = $9, memory_flush_at = $10,
			label = $11, spawned_by = $12, spawn_depth = $13,
			agent_id = $14, user_id = $15, metadata = $16, updated_at = $17
		 WHERE session_key = $18`,
		msgsJSON, nilStr(snapshot.Summary), nilStr(snapshot.Model), nilStr(snapshot.Provider), nilStr(snapshot.Channel),
		snapshot.InputTokens, snapshot.OutputTokens, snapshot.CompactionCount,
		snapshot.MemoryFlushCompactionCount, snapshot.MemoryFlushAt,
		nilStr(snapshot.Label), nilStr(snapshot.SpawnedBy), snapshot.SpawnDepth,
		nilSessionUUID(snapshot.AgentUUID), nilStr(snapshot.UserID), metaJSON, snapshot.Updated,
		key,
	)
	return err
}

func (s *PGSessionStore) LastUsedChannel(agentID string) (string, string) {
	prefix := "agent:" + agentID + ":%"
	var sessionKey string
	err := s.db.QueryRow(
		`SELECT session_key FROM sessions
		 WHERE session_key LIKE $1
		   AND session_key NOT LIKE $2
		   AND session_key NOT LIKE $3
		 ORDER BY updated_at DESC LIMIT 1`,
		prefix,
		"agent:"+agentID+":cron:%",
		"agent:"+agentID+":subagent:%",
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

func (s *PGSessionStore) getOrInit(key string) *store.SessionData {
	if data, ok := s.cache[key]; ok {
		return data
	}

	// Try loading from DB first to avoid overwriting existing messages
	data := s.loadFromDB(key)
	if data != nil {
		s.cache[key] = data
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
	s.cache[key] = data

	msgsJSON, _ := json.Marshal([]providers.Message{})
	s.db.Exec(
		`INSERT INTO sessions (id, session_key, messages, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5) ON CONFLICT (session_key) DO NOTHING`,
		uuid.Must(uuid.NewV7()), key, msgsJSON, now, now,
	)
	return data
}

func (s *PGSessionStore) loadFromDB(key string) *store.SessionData {
	var sessionKey string
	var msgsJSON []byte
	var summary, model, provider, channel, label, spawnedBy, userID *string
	var agentID *uuid.UUID
	var inputTokens, outputTokens int64
	var compactionCount, memoryFlushCompactionCount, spawnDepth int
	var memoryFlushAt int64
	var createdAt, updatedAt time.Time
	var metaJSON *[]byte

	err := s.db.QueryRow(
		`SELECT session_key, messages, summary, model, provider, channel,
		 input_tokens, output_tokens, compaction_count,
		 memory_flush_compaction_count, memory_flush_at,
		 label, spawned_by, spawn_depth, agent_id, user_id,
		 COALESCE(metadata, '{}'), created_at, updated_at
		 FROM sessions WHERE session_key = $1`, key,
	).Scan(&sessionKey, &msgsJSON, &summary, &model, &provider, &channel,
		&inputTokens, &outputTokens, &compactionCount,
		&memoryFlushCompactionCount, &memoryFlushAt,
		&label, &spawnedBy, &spawnDepth, &agentID, &userID,
		&metaJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil
	}

	var msgs []providers.Message
	json.Unmarshal(msgsJSON, &msgs)

	var meta map[string]string
	if metaJSON != nil {
		json.Unmarshal(*metaJSON, &meta)
	}

	return &store.SessionData{
		Key:                        sessionKey,
		Messages:                   msgs,
		Summary:                    derefStr(summary),
		Created:                    createdAt,
		Updated:                    updatedAt,
		AgentUUID:                  derefUUID(agentID),
		UserID:                     derefStr(userID),
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
	}
}

func nilSessionUUID(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}
