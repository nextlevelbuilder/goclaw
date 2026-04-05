//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteSubagentTaskStore is a no-op implementation for the desktop/lite edition.
// Subagent task persistence is a standard-edition feature.
type SQLiteSubagentTaskStore struct{}

func NewSQLiteSubagentTaskStore() *SQLiteSubagentTaskStore { return &SQLiteSubagentTaskStore{} }

func (s *SQLiteSubagentTaskStore) Create(context.Context, *store.SubagentTaskData) error { return nil }
func (s *SQLiteSubagentTaskStore) Get(context.Context, uuid.UUID) (*store.SubagentTaskData, error) {
	return nil, nil
}
func (s *SQLiteSubagentTaskStore) UpdateStatus(context.Context, uuid.UUID, string, *string, int, int64, int64) error {
	return nil
}
func (s *SQLiteSubagentTaskStore) ListByParent(context.Context, string, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (s *SQLiteSubagentTaskStore) ListBySession(context.Context, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (s *SQLiteSubagentTaskStore) Archive(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
func (s *SQLiteSubagentTaskStore) UpdateMetadata(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
