package pg

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ManagedToolGrantInfo is a simplified grant record for API responses.
type ManagedToolGrantInfo struct {
	ManagedToolID uuid.UUID `json:"managed_tool_id"`
	PinnedVersion int       `json:"pinned_version"`
}

// GrantManagedToolToAgent grants a managed tool to an agent with optional version pinning.
func (s *PGManagedToolStore) GrantManagedToolToAgent(ctx context.Context, managedToolID, agentID uuid.UUID, version *int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO managed_tool_agent_grants (id, managed_tool_id, agent_id, pinned_version, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (managed_tool_id, agent_id) DO UPDATE SET pinned_version = EXCLUDED.pinned_version`,
		store.GenNewID(), managedToolID, agentID, version, time.Now(),
	)
	if err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// RevokeManagedToolFromAgent revokes a managed tool grant from an agent.
func (s *PGManagedToolStore) RevokeManagedToolFromAgent(ctx context.Context, managedToolID, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM managed_tool_agent_grants WHERE managed_tool_id = $1 AND agent_id = $2",
		managedToolID, agentID)
	if err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// ListManagedToolAgentGrants returns all managed tool grants for an agent.
func (s *PGManagedToolStore) ListManagedToolAgentGrants(ctx context.Context, agentID uuid.UUID) ([]ManagedToolGrantInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT managed_tool_id, pinned_version FROM managed_tool_agent_grants WHERE agent_id = $1",
		agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ManagedToolGrantInfo
	for rows.Next() {
		var g ManagedToolGrantInfo
		if err := rows.Scan(&g.ManagedToolID, &g.PinnedVersion); err != nil {
			slog.Warn("managed_tool_grants: scan error in ListManagedToolAgentGrants", "error", err)
			continue
		}
		result = append(result, g)
	}
	return result, rows.Err()
}
