package pg

import (
	"database/sql"
	"encoding/json"
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
	return &PGSessionStore{
		db:    db,
		cache: make(map[string]*store.SessionData),
	}
}

func (s *PGSessionStore) GetOrCreate(key string) *store.SessionData {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cached, ok := s.cache[key]; ok {
		return cached
	}

	data := s.loadFromDB(key)
	if data != nil {
		s.cache[key] = data
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
	s.cache[key] = data

	msgsJSON, _ := json.Marshal([]providers.Message{})
	s.db.Exec(
		`INSERT INTO sessions (id, session_key, messages, created_at, updated_at, team_id)
		 VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (session_key) DO NOTHING`,
		uuid.Must(uuid.NewV7()), key, msgsJSON, now, now, teamID,
	)

	return data
}

func (s *PGSessionStore) AddMessage(key string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.getOrInit(key)
	data.Messages = append(data.Messages, msg)
	data.Updated = time.Now()
}

func (s *PGSessionStore) GetHistory(key string) []providers.Message {
	s.mu.RLock()
	if data, ok := s.cache[key]; ok {
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
	if data, ok := s.cache[key]; ok {
		msgs := make([]providers.Message, len(data.Messages))
		copy(msgs, data.Messages)
		return msgs
	}

	data := s.loadFromDB(key)
	if data == nil {
		return nil
	}
	s.cache[key] = data
	msgs := make([]providers.Message, len(data.Messages))
	copy(msgs, data.Messages)
	return msgs
}

func (s *PGSessionStore) GetSummary(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.cache[key]; ok {
		return data.Summary
	}
	return ""
}

func (s *PGSessionStore) SetSummary(key, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[key]; ok {
		data.Summary = summary
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) SetLabel(key, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[key]; ok {
		data.Label = label
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) SetSessionMetadata(key string, metadata map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.getOrInit(key)
	if data.Metadata == nil {
		data.Metadata = make(map[string]string)
	}
	maps.Copy(data.Metadata, metadata)
	data.Updated = time.Now()
}

func (s *PGSessionStore) SetAgentInfo(key string, agentUUID uuid.UUID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.getOrInit(key)
	if agentUUID != uuid.Nil {
		data.AgentUUID = agentUUID
	}
	if userID != "" {
		data.UserID = userID
	}
}

func (s *PGSessionStore) UpdateMetadata(key, model, provider, channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[key]; ok {
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
