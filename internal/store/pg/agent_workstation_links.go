package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGAgentWorkstationLinkStore implements store.AgentWorkstationLinkStore backed by PostgreSQL.
type PGAgentWorkstationLinkStore struct {
	db *sql.DB
}

// NewPGAgentWorkstationLinkStore creates a PGAgentWorkstationLinkStore.
func NewPGAgentWorkstationLinkStore(db *sql.DB) *PGAgentWorkstationLinkStore {
	return &PGAgentWorkstationLinkStore{db: db}
}

func (s *PGAgentWorkstationLinkStore) Link(ctx context.Context, link *store.AgentWorkstationLink) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	link.TenantID = tid
	link.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_workstation_links (agent_id, workstation_id, tenant_id, is_default, created_at)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (agent_id, workstation_id) DO NOTHING`,
		link.AgentID, link.WorkstationID, tid, link.IsDefault, link.CreatedAt,
	)
	return err
}

func (s *PGAgentWorkstationLinkStore) Unlink(ctx context.Context, agentID, workstationID uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_workstation_links WHERE agent_id = $1 AND workstation_id = $2 AND tenant_id = $3`,
		agentID, workstationID, tid,
	)
	return err
}

func (s *PGAgentWorkstationLinkStore) SetDefault(ctx context.Context, agentID, workstationID uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Clear previous default for this agent.
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent_workstation_links SET is_default = FALSE
		 WHERE agent_id = $1 AND tenant_id = $2`,
		agentID, tid,
	); err != nil {
		tx.Rollback()
		return err
	}
	// Set new default.
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent_workstation_links SET is_default = TRUE
		 WHERE agent_id = $1 AND workstation_id = $2 AND tenant_id = $3`,
		agentID, workstationID, tid,
	); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *PGAgentWorkstationLinkStore) ListForAgent(ctx context.Context, agentID uuid.UUID) ([]store.AgentWorkstationLink, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, workstation_id, tenant_id, is_default, created_at
		 FROM agent_workstation_links WHERE agent_id = $1 AND tenant_id = $2`,
		agentID, tid,
	)
	if err != nil {
		return nil, err
	}
	return scanLinks(rows)
}

func (s *PGAgentWorkstationLinkStore) ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]store.AgentWorkstationLink, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, workstation_id, tenant_id, is_default, created_at
		 FROM agent_workstation_links WHERE workstation_id = $1 AND tenant_id = $2`,
		workstationID, tid,
	)
	if err != nil {
		return nil, err
	}
	return scanLinks(rows)
}

func scanLinks(rows *sql.Rows) ([]store.AgentWorkstationLink, error) {
	defer rows.Close()
	var result []store.AgentWorkstationLink
	for rows.Next() {
		var l store.AgentWorkstationLink
		if err := rows.Scan(&l.AgentID, &l.WorkstationID, &l.TenantID, &l.IsDefault, &l.CreatedAt); err != nil {
			continue
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
