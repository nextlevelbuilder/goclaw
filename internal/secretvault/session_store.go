package secretvault

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DefaultSessionTTL is how long a session-scoped secret lives in memory.
const DefaultSessionTTL = 30 * time.Minute

type sessionKey struct {
	tenant  uuid.UUID
	session string
	name    string
}

type sessionEntry struct {
	entry    store.SecretVaultEntry
	username []byte
	secret   []byte
	expires  time.Time
}

func (e *sessionEntry) wipe() {
	clear(e.username)
	clear(e.secret)
}

// SessionStore keeps secrets that were provided for one chat session only.
// Values live in memory, never reach the database, expire after the TTL and
// are overwritten with zeros when dropped.
type SessionStore struct {
	mu  sync.Mutex
	m   map[sessionKey]*sessionEntry
	ttl time.Duration
	now func() time.Time
}

// NewSessionStore builds a store; ttl <= 0 selects DefaultSessionTTL.
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &SessionStore{m: make(map[sessionKey]*sessionEntry), ttl: ttl, now: time.Now}
}

// Put stores (or replaces) a secret for (tenant, session, entry.Name).
func (s *SessionStore) Put(tenant uuid.UUID, session string, e store.SecretVaultEntry, p store.SecretVaultPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	k := sessionKey{tenant, session, e.Name}
	if old, ok := s.m[k]; ok {
		old.wipe()
	}
	s.m[k] = &sessionEntry{
		entry:    e,
		username: []byte(p.Username),
		secret:   []byte(p.Secret),
		expires:  s.now().Add(s.ttl),
	}
}

// Get returns a live session secret. The payload is a fresh copy.
func (s *SessionStore) Get(tenant uuid.UUID, session, name string) (store.SecretVaultEntry, store.SecretVaultPayload, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	e, ok := s.m[sessionKey{tenant, session, name}]
	if !ok {
		return store.SecretVaultEntry{}, store.SecretVaultPayload{}, false
	}
	return e.entry, store.SecretVaultPayload{Username: string(e.username), Secret: string(e.secret)}, true
}

// List returns every live secret of a session.
func (s *SessionStore) List(tenant uuid.UUID, session string) []store.SecretVaultResolved {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	var out []store.SecretVaultResolved
	for k, e := range s.m {
		if k.tenant == tenant && k.session == session {
			out = append(out, store.SecretVaultResolved{
				Entry:   e.entry,
				Payload: store.SecretVaultPayload{Username: string(e.username), Secret: string(e.secret)},
			})
		}
	}
	return out
}

// Drop wipes every secret of a session (call when the session ends).
func (s *SessionStore) Drop(tenant uuid.UUID, session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.m {
		if k.tenant == tenant && k.session == session {
			e.wipe()
			delete(s.m, k)
		}
	}
}

// Sweep removes expired entries; it also runs on every other call.
func (s *SessionStore) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
}

func (s *SessionStore) sweepLocked() {
	now := s.now()
	for k, e := range s.m {
		if !now.Before(e.expires) {
			e.wipe()
			delete(s.m, k)
		}
	}
}
