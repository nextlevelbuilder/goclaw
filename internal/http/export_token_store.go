package http

import (
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ExportTokenStore manages short-lived export tokens with proper lifecycle.
// Shared across agent export, backup, and tenant backup handlers.
type ExportTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*exportToken
	stop   chan struct{}
}

// NewExportTokenStore creates a token store and starts the background sweep goroutine.
func NewExportTokenStore() *ExportTokenStore {
	s := &ExportTokenStore{
		tokens: make(map[string]*exportToken),
		stop:   make(chan struct{}),
	}
	go s.sweep()
	return s
}

// Store creates a short-lived token referencing a temp export file.
func (s *ExportTokenStore) Store(entityID, userID, filePath, fileName string) string {
	token := uuid.Must(uuid.NewV7()).String()
	entry := &exportToken{
		agentID:   entityID,
		userID:    userID,
		filePath:  filePath,
		fileName:  fileName,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
	s.mu.Lock()
	s.tokens[token] = entry
	s.mu.Unlock()
	return token
}

// Get retrieves a token entry, removing it if expired. Does not consume the token.
func (s *ExportTokenStore) Get(token string) (*exportToken, bool) {
	s.mu.Lock()
	entry, ok := s.tokens[token]
	if ok && time.Now().After(entry.expiresAt) {
		delete(s.tokens, token)
		ok = false
	}
	s.mu.Unlock()
	return entry, ok
}

// Stop terminates the sweep goroutine and cleans up remaining temp files.
func (s *ExportTokenStore) Stop() {
	close(s.stop)
	s.mu.Lock()
	for _, e := range s.tokens {
		os.Remove(e.filePath) //nolint:errcheck
	}
	s.tokens = nil
	s.mu.Unlock()
}

// sweep runs every 60s, removes expired tokens and their temp files.
func (s *ExportTokenStore) sweep() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for tok, e := range s.tokens {
				if now.After(e.expiresAt) {
					os.Remove(e.filePath) //nolint:errcheck
					delete(s.tokens, tok)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Package-level singleton — initialized by InitExportTokenStore(), stopped by calling Stop().
var exportTokenStore *ExportTokenStore

// InitExportTokenStore creates and starts the global export token store.
// Returns the store so the caller can defer Stop().
func InitExportTokenStore() *ExportTokenStore {
	exportTokenStore = NewExportTokenStore()
	return exportTokenStore
}

// storeExportToken creates a short-lived token via the global store.
func storeExportToken(entityID, userID, filePath, fileName string) string {
	return exportTokenStore.Store(entityID, userID, filePath, fileName)
}

// lookupExportToken retrieves and validates a token from the global store.
func lookupExportToken(token string) (*exportToken, bool) {
	return exportTokenStore.Get(token)
}
