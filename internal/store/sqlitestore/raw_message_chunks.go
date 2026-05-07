//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteRawMessageChunkStore is a no-op stub — SQLite does not support pgvector.
type SQLiteRawMessageChunkStore struct{}

func NewSQLiteRawMessageChunkStore() *SQLiteRawMessageChunkStore {
	return &SQLiteRawMessageChunkStore{}
}

func (s *SQLiteRawMessageChunkStore) StoreChunks(_ context.Context, _ []store.RawMessageChunk, _ [][]float32) error {
	return nil
}

func (s *SQLiteRawMessageChunkStore) Search(_ context.Context, _ string, _ string, _ store.RawMessageChunkSearchOptions) ([]store.RawMessageChunkSearchResult, error) {
	return nil, nil
}

func (s *SQLiteRawMessageChunkStore) DeleteByGraphID(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *SQLiteRawMessageChunkStore) List(_ context.Context, _ store.RawMessageChunkListOpts) ([]store.RawMessageChunk, int, error) {
	return nil, 0, nil
}

func (s *SQLiteRawMessageChunkStore) DeleteByIDs(_ context.Context, _ []string) (int64, error) {
	return 0, nil
}

func (s *SQLiteRawMessageChunkStore) DeleteByChatID(_ context.Context, _ string, _ string) (int64, error) {
	return 0, nil
}

func (s *SQLiteRawMessageChunkStore) SetEmbeddingProvider(_ store.EmbeddingProvider) {}

func (s *SQLiteRawMessageChunkStore) ReEmbedChunks(_ context.Context, _ store.RawMessageChunkListOpts) (int, int, error) {
	return 0, 0, nil
}
