package wechat

import (
	"sync"
)

// contextTokenStore manages per-account per-user context tokens in memory.
// The Weixin API requires echoing the context_token from inbound messages
// in every outbound send to maintain conversation context.
type contextTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]string // key: "accountId:userId" -> context_token
}

func newContextTokenStore() *contextTokenStore {
	return &contextTokenStore{
		tokens: make(map[string]string),
	}
}

func tokenKey(accountID, userID string) string {
	return accountID + ":" + userID
}

// Set stores a context token for an account+user pair.
func (s *contextTokenStore) Set(accountID, userID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tokenKey(accountID, userID)] = token
}

// Get retrieves the cached context token for an account+user pair.
func (s *contextTokenStore) Get(accountID, userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokens[tokenKey(accountID, userID)]
}

// Clear removes all tokens for a given account.
func (s *contextTokenStore) Clear(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := accountID + ":"
	for k := range s.tokens {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(s.tokens, k)
		}
	}
}
