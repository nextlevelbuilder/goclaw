package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGSessionStore implements store.SessionStore backed by Postgres.
type PGSessionStore struct {
	db *sql.DB
	mu sync.RWMutex
	// In-memory cache for hot sessions (reduces DB reads during tool loops)
	cache map[string]*store.SessionData
	// OnDelete is called with the session key when a session is deleted.
	// Used for media file cleanup.
	OnDelete func(sessionKey string)
}

func NewPGSessionStore(db *sql.DB) *PGSessionStore {
	s := &PGSessionStore{
		db:    db,
		cache: make(map[string]*store.SessionData),
	}
	s.migrateLegacyWSKeys()
	return s
}

// migrateLegacyWSKeys renames old WS session keys from non-canonical format
// (agent:X:ws-userId-ts) to canonical format (agent:X:ws:direct:ts).
// The last hyphen-delimited segment is the base36 timestamp used as convId.
// Idempotent — no-op if no legacy keys exist.
func (s *PGSessionStore) migrateLegacyWSKeys() {
	res, err := s.db.ExecContext(context.Background(), `
		UPDATE sessions
		SET session_key = regexp_replace(
			session_key,
			'^(agent:[^:]+):ws-.+-([^-]+)$',
			'\1:ws:direct:\2'
		)
		WHERE session_key ~ '^agent:[^:]+:ws-'
	`)
	if err != nil {
		slog.Warn("sessions.migrate_legacy_ws_keys", "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("sessions.migrate_legacy_ws_keys", "migrated", n)
	}
}

// sessionCacheKey prefixes session key with tenant UUID to prevent cross-tenant cache collisions.
// Two tenants with the same agent_key produce different cache keys.
func sessionCacheKey(ctx context.Context, key string) string {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	return tid.String() + ":" + key
}

func (s *PGSessionStore) GetOrCreate(ctx context.Context, key string) *store.SessionData {
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

	// Create new
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (tenant_id, session_key) DO NOTHING`,
		uuid.Must(uuid.NewV7()), key, msgsJSON, now, now, teamID, tenantIDForInsert(ctx),
	)

	return data
}

// Get returns the session if it exists (cache or DB), nil otherwise. Never creates.
func (s *PGSessionStore) Get(ctx context.Context, key string) *store.SessionData {
	s.mu.RLock()
	if cached, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return cached
	}

	data := s.loadFromDB(ctx, key)
	if data != nil {
		s.cache[sessionCacheKey(ctx, key)] = data
	}
	return data
}

func (s *PGSessionStore) AddMessage(ctx context.Context, key string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stamp message creation time if not already set.
	if msg.CreatedAt == nil {
		now := time.Now().UTC()
		msg.CreatedAt = &now
	}

	data := s.getOrInit(ctx, key)
	data.Messages = append(data.Messages, msg)
	data.Updated = time.Now()
}

func (s *PGSessionStore) GetHistory(ctx context.Context, key string) []providers.Message {
	s.mu.RLock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		msgs := make([]providers.Message, len(data.Messages))
		copy(msgs, data.Messages)
		s.mu.RUnlock()
		return msgs
	}
	s.mu.RUnlock()

	// Not in cache — load from DB and cache it
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
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

func (s *PGSessionStore) GetSummary(ctx context.Context, key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return data.Summary
	}
	return ""
}

func (s *PGSessionStore) SetSummary(ctx context.Context, key, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Summary = summary
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) GetLabel(ctx context.Context, key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		return data.Label
	}
	return ""
}

func (s *PGSessionStore) SetLabel(ctx context.Context, key, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Label = label
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) GetSessionMetadata(ctx context.Context, key string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok && data.Metadata != nil {
		out := make(map[string]string, len(data.Metadata))
		maps.Copy(out, data.Metadata)
		return out
	}
	return nil
}

func (s *PGSessionStore) SetSessionMetadata(ctx context.Context, key string, metadata map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.getOrInit(ctx, key)
	if data.Metadata == nil {
		data.Metadata = make(map[string]string)
	}
	maps.Copy(data.Metadata, metadata)
	data.Updated = time.Now()
}

func (s *PGSessionStore) SetAgentInfo(ctx context.Context, key string, agentUUID uuid.UUID, userID string) {
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

func (s *PGSessionStore) UpdateMetadata(ctx context.Context, key, model, provider, channel string) {
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
