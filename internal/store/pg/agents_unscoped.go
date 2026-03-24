package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GetByIDUnscoped returns an agent by ID without tenant filtering. Server-internal only.
func (s *PGAgentStore) GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+agentSelectCols+`
		 FROM agents WHERE id = $1 AND deleted_at IS NULL`, id)
	d, err := scanAgentRow(row)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", id)
	}
	return d, nil
}
