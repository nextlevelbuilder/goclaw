package pg

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListAllInstances returns all channel instances across all tenants. Server-internal only.
func (s *PGChannelInstanceStore) ListAllInstances(ctx context.Context) ([]store.ChannelInstanceData, error) {
	q := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}

// ListAllEnabled returns enabled channel instances across all tenants. Server-internal only.
func (s *PGChannelInstanceStore) ListAllEnabled(ctx context.Context) ([]store.ChannelInstanceData, error) {
	q := `SELECT ` + channelInstanceSelectCols + ` FROM channel_instances WHERE enabled = true ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.scanInstances(rows)
}
