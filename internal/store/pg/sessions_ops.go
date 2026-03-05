package pg

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func (s *PGSessionStore) TruncateHistory(key string, keepLast int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[key]; ok {
		if keepLast <= 0 {
			data.Messages = []providers.Message{}
		} else if len(data.Messages) > keepLast {
			data.Messages = data.Messages[len(data.Messages)-keepLast:]
		}
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) Reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[key]; ok {
		data.Messages = []providers.Message{}
		data.Summary = ""
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) Delete(key string) error {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM sessions WHERE session_key = $1", key)
	return err
}
