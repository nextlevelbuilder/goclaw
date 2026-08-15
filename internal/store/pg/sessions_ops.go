package pg

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func (s *PGSessionStore) TruncateHistory(ctx context.Context, key string, keepLast int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		if keepLast <= 0 {
			data.Messages = []providers.Message{}
		} else if len(data.Messages) > keepLast {
			data.Messages = data.Messages[len(data.Messages)-keepLast:]
		}
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) SetHistory(ctx context.Context, key string, msgs []providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Messages = msgs
		data.Updated = time.Now()
	}
}

func (s *PGSessionStore) Reset(ctx context.Context, key string) {
	s.mu.Lock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Messages = []providers.Message{}
		data.Summary = ""
		data.Updated = time.Now()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Session not in cache (e.g. after server restart). Clear directly in DB
	// so the next GetOrCreate loads a clean session instead of stale history.
	tid := tenantIDForInsert(ctx)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET messages = '[]', summary = '', updated_at = $1
		 WHERE session_key = $2 AND tenant_id = $3`,
		time.Now(), key, tid,
	); err != nil {
		slog.Warn("sessions.reset_db_fallback_failed", "key", key, "error", err)
	}
}

func (s *PGSessionStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.cache, sessionCacheKey(ctx, key))
	s.mu.Unlock()

	// Clean up associated media files before deleting from DB.
	if s.OnDelete != nil {
		s.OnDelete(key)
	}

	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", key, tid)
	return err
}
