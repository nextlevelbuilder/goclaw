//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteAgentWorkstationLinkStore implements store.AgentWorkstationLinkStore backed by SQLite.
type SQLiteAgentWorkstationLinkStore struct {
	db *sql.DB
}

// NewSQLiteAgentWorkstationLinkStore creates a SQLiteAgentWorkstationLinkStore.
func NewSQLiteAgentWorkstationLinkStore(db *sql.DB) *SQLiteAgentWorkstationLinkStore {
	return &SQLiteAgentWorkstationLinkStore{db: db}
}

func (s *SQLiteAgentWorkstationLinkStore) Link(ctx context.Context, link *store.AgentWorkstationLink) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	link.TenantID = tid
	link.CreatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO agent_workstation_links
		 (agent_id, workstation_id, tenant_id, is_default, created_at)
		 VALUES (?,?,?,?,?)`,
		link.AgentID.String(), link.WorkstationID.String(), tid.String(),
		boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteAgentWorkstationLinkStore) Unlink(ctx context.Context, agentID, workstationID uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_workstation_links WHERE agent_id = ? AND workstation_id = ? AND tenant_id = ?`,
		agentID.String(), workstationID.String(), tid.String(),
	)
	return err
}

func (s *SQLiteAgentWorkstationLinkStore) SetDefault(ctx context.Context, agentID, workstationID uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent_workstation_links SET is_default = 0 WHERE agent_id = ? AND tenant_id = ?`,
		agentID.String(), tid.String(),
	); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent_workstation_links SET is_default = 1
		 WHERE agent_id = ? AND workstation_id = ? AND tenant_id = ?`,
		agentID.String(), workstationID.String(), tid.String(),
	); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLiteAgentWorkstationLinkStore) ListForAgent(ctx context.Context, agentID uuid.UUID) ([]store.AgentWorkstationLink, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, workstation_id, tenant_id, is_default, created_at
		 FROM agent_workstation_links WHERE agent_id = ? AND tenant_id = ?`,
		agentID.String(), tid.String(),
	)
	if err != nil {
		return nil, err
	}
	return scanSQLiteLinks(rows)
}

func (s *SQLiteAgentWorkstationLinkStore) ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]store.AgentWorkstationLink, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, workstation_id, tenant_id, is_default, created_at
		 FROM agent_workstation_links WHERE workstation_id = ? AND tenant_id = ?`,
		workstationID.String(), tid.String(),
	)
	if err != nil {
		return nil, err
	}
	return scanSQLiteLinks(rows)
}

func scanSQLiteLinks(rows *sql.Rows) ([]store.AgentWorkstationLink, error) {
	defer rows.Close()
	var result []store.AgentWorkstationLink
	for rows.Next() {
		var l store.AgentWorkstationLink
		var agentStr, wsStr, tenantStr string
		var isDefaultInt int
		var createdAt sqliteTime
		if err := rows.Scan(&agentStr, &wsStr, &tenantStr, &isDefaultInt, &createdAt); err != nil {
			continue
		}
		l.AgentID, _ = uuid.Parse(agentStr)
		l.WorkstationID, _ = uuid.Parse(wsStr)
		l.TenantID, _ = uuid.Parse(tenantStr)
		l.IsDefault = isDefaultInt != 0
		l.CreatedAt = createdAt.Time
		result = append(result, l)
	}
	return result, rows.Err()
}
