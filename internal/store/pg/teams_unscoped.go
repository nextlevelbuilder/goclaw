package pg

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GetTeamUnscoped returns a team by ID without tenant filtering. Server-internal only.
func (s *PGTeamStore) GetTeamUnscoped(ctx context.Context, id uuid.UUID) (*store.TeamData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+teamSelectCols+` FROM agent_teams WHERE id = $1`, id)
	return scanTeamRow(row)
}
