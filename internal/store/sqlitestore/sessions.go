//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteSessionStore implements store.SessionStore backed by SQLite.
type SQLiteSessionStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]*store.SessionData
	// OnDelete is called with the session key when a session is deleted.
	OnDelete func(sessionKey string)
}

func NewSQLiteSessionStore(db *sql.DB) *SQLiteSessionStore {
	// No migrateLegacyWSKeys — SQLite has no regexp_replace.
	return &SQLiteSessionStore{db: db, cache: make(map[string]*store.SessionData)}
}

// sessionCacheKey prefixes session key with tenant UUID to prevent cross-tenant cache collisions.
func sessionCacheKey(ctx context.Context, key string) string {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	return tid.String() + ":" + key
}

func (s *SQLiteSessionStore) GetOrCreate(ctx context.Context, key string) *store.SessionData {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cached, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return cached
	}

	data := s.loadFromDB(ctx, key)
	if data != nil {
		s.cache[sessionCacheKey(ctx, key)] = data
		return data
	}

	now := time.Now()
	data = &store.SessionData{
		Key:      key,
		Messages: []providers.Message{},
		Created:  now,
		Updated:  now,
	}

	// Extract team_id from team session keys (agent:{agentId}:team:{teamId}:{chatId}).
	var teamID *uuid.UUID
	if parts := strings.SplitN(key, ":", 5); len(parts) >= 4 && parts[2] == "team" {
		if tid, err := uuid.Parse(parts[3]); err == nil {
			teamID = &tid
			data.TeamID = teamID
		}
	}
	s.cache[sessionCacheKey(ctx, key)] = data

	msgsJSON, _ := json.Marshal([]providers.Message{})
	s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, session_key, messages, created_at, updated_at, team_id, tenant_id)
		 VALUES (?,?,?,?,?,?,?) ON CONFLICT (tenant_id, session_key) DO NOTHING`,
		uuid.Must(uuid.NewV7()), key, msgsJSON, now, now, teamID, tenantIDForInsert(ctx),
	)

	return data
}

// Get returns the session if it exists (cache or DB), nil otherwise. Never creates.
func (s *SQLiteSessionStore) Get(ctx context.Context, key string) *store.SessionData {
	s.mu.RLock()
	if cached, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if cached, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return cached
	}

	data := s.loadFromDB(ctx, key)
	if data != nil {
		s.cache[sessionCacheKey(ctx, key)] = data
	}
	return data
}

func (s *SQLiteSessionStore) AddMessage(ctx context.Context, key string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.CreatedAt == nil {
		now := time.Now().UTC()
		msg.CreatedAt = &now
	}

	data := s.getOrInit(ctx, key)
	data.Messages = append(data.Messages, msg)
	data.Updated = time.Now()
}

func (s *SQLiteSessionStore) GetHistory(ctx context.Context, key string) []providers.Message {
	s.mu.RLock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		msgs := make([]providers.Message, len(data.Messages))
		copy(msgs, data.Messages)
		s.mu.RUnlock()
		return msgs
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		msgs := make([]providers.Message, len(data.Messages))
		copy(msgs, data.Messages)
		return msgs
	}

	data := s.loadFromDB(ctx, key)
	if data == nil {
		return nil
	}
	s.cache[sessionCacheKey(ctx, key)] = data
	msgs := make([]providers.Message, len(data.Messages))
	copy(msgs, data.Messages)
	return msgs
}

func (s *SQLiteSessionStore) GetSummary(ctx context.Context, key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return data.Summary
	}
	return ""
}

func (s *SQLiteSessionStore) SetSummary(ctx context.Context, key, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Summary = summary
		data.Updated = time.Now()
	}
}

func (s *SQLiteSessionStore) GetLabel(ctx context.Context, key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return data.Label
	}
	return ""
}

func (s *SQLiteSessionStore) SetLabel(ctx context.Context, key, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Label = label
		data.Updated = time.Now()
	}
}

func (s *SQLiteSessionStore) GetSessionMetadata(ctx context.Context, key string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok && data.Metadata != nil {
		out := make(map[string]string, len(data.Metadata))
		maps.Copy(out, data.Metadata)
		return out
	}
	return nil
}

func (s *SQLiteSessionStore) SetSessionMetadata(ctx context.Context, key string, metadata map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.getOrInit(ctx, key)
	if data.Metadata == nil {
		data.Metadata = make(map[string]string)
	}
	maps.Copy(data.Metadata, metadata)
	data.Updated = time.Now()
}

func (s *SQLiteSessionStore) SetAgentInfo(ctx context.Context, key string, agentUUID uuid.UUID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.getOrInit(ctx, key)
	if agentUUID != uuid.Nil {
		data.AgentUUID = agentUUID
	}
	if userID != "" {
		data.UserID = userID
	}
}

func (s *SQLiteSessionStore) UpdateMetadata(ctx context.Context, key, model, provider, channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		if model != "" {
			data.Model = model
		}
		if provider != "" {
			data.Provider = provider
		}
		if channel != "" {
			data.Channel = channel
		}
	}
}

// --- helpers ---

func (s *SQLiteSessionStore) getOrInit(ctx context.Context, key string) *store.SessionData {
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return data
	}

	data := s.loadFromDB(ctx, key)
	if data != nil {
		s.cache[sessionCacheKey(ctx, key)] = data
		return data
	}

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
		 VALUES (?,?,?,?,?,?) ON CONFLICT (tenant_id, session_key) DO NOTHING`,
		uuid.Must(uuid.NewV7()), key, msgsJSON, now, now, tenantIDForInsert(ctx),
	)
	return data
}

func (s *SQLiteSessionStore) loadFromDB(ctx context.Context, key string) *store.SessionData {
	var sessionKey string
	var msgsJSON []byte
	var summary, model, provider, channel, label, spawnedBy, userID *string
	var agentID, teamID *uuid.UUID
	var inputTokens, outputTokens int64
	var compactionCount, memoryFlushCompactionCount, spawnDepth int
	var memoryFlushAt int64
	createdAt, updatedAt := scanTimePair()
	var metaJSON *[]byte

	tid := tenantIDForInsert(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT session_key, messages, summary, model, provider, channel,
		 input_tokens, output_tokens, compaction_count,
		 memory_flush_compaction_count, memory_flush_at,
		 label, spawned_by, spawn_depth, agent_id, user_id,
		 COALESCE(metadata, '{}'), created_at, updated_at, team_id
		 FROM sessions WHERE session_key = ? AND tenant_id = ?`, key, tid,
	).Scan(&sessionKey, &msgsJSON, &summary, &model, &provider, &channel,
		&inputTokens, &outputTokens, &compactionCount,
		&memoryFlushCompactionCount, &memoryFlushAt,
		&label, &spawnedBy, &spawnDepth, &agentID, &userID,
		&metaJSON, createdAt, updatedAt, &teamID)
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
		Created:                    createdAt.Time,
		Updated:                    updatedAt.Time,
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
