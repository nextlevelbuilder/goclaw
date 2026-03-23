package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Session stores conversation history for one agent+scope combination.
type Session struct {
	Key      string              `json:"key"`       // agent:{agentId}:{sessionKey}
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`

	// Metadata (matching TS SessionEntry subset)
	Model           string `json:"model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Channel         string `json:"channel,omitempty"`
	InputTokens     int64  `json:"inputTokens,omitempty"`
	OutputTokens    int64  `json:"outputTokens,omitempty"`
	CompactionCount             int    `json:"compactionCount,omitempty"`
	MemoryFlushCompactionCount int    `json:"memoryFlushCompactionCount,omitempty"`
	MemoryFlushAt              int64  `json:"memoryFlushAt,omitempty"` // unix ms
	Label                      string `json:"label,omitempty"`
	SpawnedBy       string `json:"spawnedBy,omitempty"`
	SpawnDepth      int    `json:"spawnDepth,omitempty"`

	ContextWindow    int `json:"contextWindow,omitempty"`
	LastPromptTokens int `json:"lastPromptTokens,omitempty"`
	LastMessageCount int `json:"lastMessageCount,omitempty"`
}

// Manager handles session lifecycle, persistence, and lookup.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	storage  string
}

func NewManager(storage string) *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
		storage:  storage,
	}
	if storage != "" {
		os.MkdirAll(storage, 0755)
		m.loadAll()
	}
	return m
}

// SessionKey builds a composite session key: agent:{agentId}:{scopeKey}
func SessionKey(agentID, scopeKey string) string {
	return fmt.Sprintf("agent:%s:%s", agentID, scopeKey)
}

// GetOrCreate returns an existing session or creates a new one.
func (m *Manager) GetOrCreate(_ context.Context, key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		return s
	}

	s := &Session{
		Key:      key,
		Messages: []providers.Message{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	m.sessions[key] = s
	return s
}

// AddMessage appends a message to a session.
func (m *Manager) AddMessage(_ context.Context, key string, msg providers.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[key]
	if !ok {
		s = &Session{
			Key:      key,
			Messages: []providers.Message{},
			Created:  time.Now(),
		}
		m.sessions[key] = s
	}

	s.Messages = append(s.Messages, msg)
	s.Updated = time.Now()
}

// GetHistory returns a copy of the message history.
func (m *Manager) GetHistory(_ context.Context, key string) []providers.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[key]
	if !ok {
		return nil
	}

	msgs := make([]providers.Message, len(s.Messages))
	copy(msgs, s.Messages)
	return msgs
}

// GetSummary returns the session summary.
func (m *Manager) GetSummary(_ context.Context, key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[key]; ok {
		return s.Summary
	}
	return ""
}

// SetSummary updates the session summary.
func (m *Manager) SetSummary(_ context.Context, key, summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.Summary = summary
		s.Updated = time.Now()
	}
}

// SetLabel updates the session label.
func (m *Manager) SetLabel(_ context.Context, key, label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.Label = label
		s.Updated = time.Now()
	}
}

// UpdateMetadata sets model/provider/channel metadata on a session.
func (m *Manager) UpdateMetadata(_ context.Context, key, model, provider, channel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		if model != "" {
			s.Model = model
		}
		if provider != "" {
			s.Provider = provider
		}
		if channel != "" {
			s.Channel = channel
		}
	}
}

// AccumulateTokens adds token counts from a completed run.
func (m *Manager) AccumulateTokens(_ context.Context, key string, inputTokens, outputTokens int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.InputTokens += inputTokens
		s.OutputTokens += outputTokens
	}
}

// IncrementCompaction bumps the compaction counter after summarization.
func (m *Manager) IncrementCompaction(_ context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.CompactionCount++
	}
}

// GetCompactionCount returns the current compaction count for a session.
func (m *Manager) GetCompactionCount(_ context.Context, key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[key]; ok {
		return s.CompactionCount
	}
	return 0
}

// GetMemoryFlushCompactionCount returns the compaction count at which memory flush last ran.
func (m *Manager) GetMemoryFlushCompactionCount(_ context.Context, key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[key]; ok {
		return s.MemoryFlushCompactionCount
	}
	return -1 // never flushed
}

// SetMemoryFlushDone records that memory flush completed at the current compaction count.
func (m *Manager) SetMemoryFlushDone(_ context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.MemoryFlushCompactionCount = s.CompactionCount
		s.MemoryFlushAt = time.Now().UnixMilli()
	}
}

// SetSpawnInfo sets subagent origin metadata on a session.
func (m *Manager) SetSpawnInfo(_ context.Context, key, spawnedBy string, depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.SpawnedBy = spawnedBy
		s.SpawnDepth = depth
	}
}

// SetContextWindow caches the agent's context window on the session.
func (m *Manager) SetContextWindow(_ context.Context, key string, cw int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.ContextWindow = cw
	}
}

// GetContextWindow returns the cached context window for a session (0 if unset).
func (m *Manager) GetContextWindow(_ context.Context, key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[key]; ok {
		return s.ContextWindow
	}
	return 0
}

// SetLastPromptTokens records actual prompt tokens from the last LLM response.
func (m *Manager) SetLastPromptTokens(_ context.Context, key string, tokens, msgCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.LastPromptTokens = tokens
		s.LastMessageCount = msgCount
	}
}

// GetLastPromptTokens returns the last known prompt tokens and message count.
func (m *Manager) GetLastPromptTokens(_ context.Context, key string) (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[key]; ok {
		return s.LastPromptTokens, s.LastMessageCount
	}
	return 0, 0
}

// TruncateHistory keeps only the last N messages.
func (m *Manager) TruncateHistory(_ context.Context, key string, keepLast int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[key]
	if !ok {
		return
	}

	if keepLast <= 0 {
		s.Messages = []providers.Message{}
	} else if len(s.Messages) > keepLast {
		s.Messages = s.Messages[len(s.Messages)-keepLast:]
	}
	s.Updated = time.Now()
}

// SetHistory replaces a session's message history with the given slice.
func (m *Manager) SetHistory(_ context.Context, key string, msgs []providers.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		s.Messages = msgs
		s.Updated = time.Now()
	}
}

// Reset clears a session's history and summary.
func (m *Manager) Reset(_ context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		s.Messages = []providers.Message{}
		s.Summary = ""
		s.Updated = time.Now()
	}
}

// Delete removes a session entirely.
func (m *Manager) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.sessions, key)
	m.mu.Unlock()

	if m.storage != "" {
		filename := sanitizeFilename(key) + ".json"
		path := filepath.Join(m.storage, filename)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// List returns metadata for all sessions, optionally filtered by agent ID.
func (m *Manager) List(_ context.Context, agentID string) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []SessionInfo
	prefix := ""
	if agentID != "" {
		prefix = "agent:" + agentID + ":"
	}

	for key, s := range m.sessions {
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		result = append(result, SessionInfo{
			Key:          key,
			MessageCount: len(s.Messages),
			Created:      s.Created,
			Updated:      s.Updated,
			Label:        s.Label,
			Channel:      s.Channel,
		})
	}
	return result
}

// LastUsedChannel finds the most recently updated channel session for an agent
// and extracts channel + chatID from the key. Returns ("", "") if none found.
func (m *Manager) LastUsedChannel(_ context.Context, agentID string) (channel, chatID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := "agent:" + agentID + ":"
	var bestKey string
	var bestUpdated time.Time

	for key, s := range m.sessions {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// Skip non-channel sessions (cron, subagent)
		rest := key[len(prefix):]
		if strings.HasPrefix(rest, "cron:") || strings.HasPrefix(rest, "subagent:") {
			continue
		}
		if s.Updated.After(bestUpdated) {
			bestUpdated = s.Updated
			bestKey = key
		}
	}

	if bestKey == "" {
		return "", ""
	}

	// Parse: agent:{agentId}:{channel}:{peerKind}:{chatId}
	parts := strings.SplitN(bestKey, ":", 5)
	if len(parts) >= 5 {
		return parts[2], parts[4]
	}
	return "", ""
}

// SessionInfo is a lightweight session descriptor for listing.
type SessionInfo struct {
	Key          string    `json:"key"`
	MessageCount int       `json:"messageCount"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
	Label        string    `json:"label,omitempty"`
	Channel      string    `json:"channel,omitempty"`
}

// Save persists a session to disk atomically.
func (m *Manager) Save(_ context.Context, key string) error {
	if m.storage == "" {
		return nil
	}

	m.mu.RLock()
	s, ok := m.sessions[key]
	if !ok {
		m.mu.RUnlock()
		return nil
	}

	// Snapshot under lock
	snapshot := Session{
		Key:             s.Key,
		Summary:         s.Summary,
		Created:         s.Created,
		Updated:         s.Updated,
		Model:           s.Model,
		Provider:        s.Provider,
		Channel:         s.Channel,
		InputTokens:     s.InputTokens,
		OutputTokens:    s.OutputTokens,
		CompactionCount: s.CompactionCount,
		MemoryFlushCompactionCount: s.MemoryFlushCompactionCount,
		MemoryFlushAt:   s.MemoryFlushAt,
		Label:           s.Label,
		SpawnedBy:       s.SpawnedBy,
		SpawnDepth:      s.SpawnDepth,
		ContextWindow:    s.ContextWindow,
		LastPromptTokens: s.LastPromptTokens,
		LastMessageCount: s.LastMessageCount,
	}
	if len(s.Messages) > 0 {
		snapshot.Messages = make([]providers.Message, len(s.Messages))
		copy(snapshot.Messages, s.Messages)
	} else {
		snapshot.Messages = []providers.Message{}
	}
	m.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	filename := sanitizeFilename(key)
	if filename == "." || !filepath.IsLocal(filename) || strings.ContainsAny(filename, `/\`) {
		return os.ErrInvalid
	}

	sessionPath := filepath.Join(m.storage, filename+".json")

	// Atomic write: temp file → rename
	tmpFile, err := os.CreateTemp(m.storage, "session-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, sessionPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (m *Manager) loadAll() {
	files, err := os.ReadDir(m.storage)
	if err != nil {
		return
	}

	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(m.storage, f.Name()))
		if err != nil {
			continue
		}

		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}

		m.sessions[s.Key] = &s
	}
}

func sanitizeFilename(key string) string {
	return strings.ReplaceAll(key, ":", "_")
}
