package pg

import (
	"context"
	"fmt"
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
			// Fork: insert compaction marker into full_messages at the truncation point.
			// CompactionCount is incremented by IncrementCompaction after this call,
			// so +1 gives the correct upcoming compaction number.
			marker := providers.Message{
				Role:    "system",
				Content: fmt.Sprintf("[Compaction #%d — model receives condensed context from this point]", data.CompactionCount+1),
			}
			data.FullMessages = append(data.FullMessages, marker)
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
	defer s.mu.Unlock()
	if data, ok := s.cache[sessionCacheKey(ctx, key)]; ok {
		data.Messages = []providers.Message{}
		data.Summary = ""
		data.Updated = time.Now()
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
