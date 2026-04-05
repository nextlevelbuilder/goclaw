//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteAgentStore) ShareAgent(ctx context.Context, agentID uuid.UUID, userID, role, grantedBy string) error {
	if err := store.ValidateUserID(userID); err != nil {
		return err
	}
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_shares (id, agent_id, user_id, role, granted_by, tenant_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (agent_id, user_id) DO UPDATE SET role = excluded.role, granted_by = excluded.granted_by`,
		store.GenNewID(), agentID, userID, role, grantedBy, tid, time.Now(),
	)
	return err
}

func (s *SQLiteAgentStore) RevokeShare(ctx context.Context, agentID uuid.UUID, userID string) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx,
			"DELETE FROM agent_shares WHERE agent_id = ? AND user_id = ?", agentID, userID)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM agent_shares WHERE agent_id = ? AND user_id = ? AND tenant_id = ?", agentID, userID, tid)
	return err
}

func (s *SQLiteAgentStore) ListShares(ctx context.Context, agentID uuid.UUID) ([]store.AgentShareData, error) {
	q := "SELECT id, agent_id, user_id, role, granted_by, created_at FROM agent_shares WHERE agent_id = ?"
	args := []any{agentID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.AgentShareData
	for rows.Next() {
		var d store.AgentShareData
		var createdAt sqliteTime
		if err := rows.Scan(&d.ID, &d.AgentID, &d.UserID, &d.Role, &d.GrantedBy, &createdAt); err != nil {
			continue
		}
		d.CreatedAt = createdAt.Time
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *SQLiteAgentStore) CanAccess(ctx context.Context, agentID uuid.UUID, userID string) (bool, string, error) {
	var ownerID string
	var isDefault bool
	var err error
	if store.IsCrossTenant(ctx) {
		err = s.db.QueryRowContext(ctx,
			"SELECT owner_id, is_default FROM agents WHERE id = ? AND deleted_at IS NULL", agentID,
		).Scan(&ownerID, &isDefault)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, "", fmt.Errorf("agent not found")
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT owner_id, is_default FROM agents WHERE id = ? AND deleted_at IS NULL AND tenant_id = ?",
			agentID, tid,
		).Scan(&ownerID, &isDefault)
	}
	if err != nil {
		return false, "", fmt.Errorf("agent not found")
	}
	if isDefault {
		if ownerID == userID {
			return true, "owner", nil
		}
		return true, "user", nil
	}
	if ownerID == userID {
		return true, "owner", nil
	}
	// Check shares
	var role string
	if store.IsCrossTenant(ctx) {
		err = s.db.QueryRowContext(ctx,
			"SELECT role FROM agent_shares WHERE agent_id = ? AND user_id = ?", agentID, userID,
		).Scan(&role)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, "", nil
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT role FROM agent_shares WHERE agent_id = ? AND user_id = ? AND tenant_id = ?",
			agentID, userID, tid,
		).Scan(&role)
	}
	if err != nil {
		return false, "", nil
	}
	return true, role, nil
}

func (s *SQLiteAgentStore) ListAccessible(ctx context.Context, userID string) ([]store.AgentData, error) {
	if store.IsCrossTenant(ctx) {
		rows, err := s.db.QueryContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents
			 WHERE deleted_at IS NULL AND (
			     owner_id = ?
			     OR is_default = 1
			     OR id IN (SELECT agent_id FROM agent_shares WHERE user_id = ?)
			     OR (agent_type = 'predefined' AND id IN (
			         SELECT agent_id FROM channel_instances ci
			         WHERE ci.enabled = 1
			         AND EXISTS (
			             SELECT 1 FROM json_each(json_extract(ci.config, '$.allow_from'))
			             WHERE json_each.value = ?
			         )
			     ))
			 )
			 ORDER BY created_at DESC`, userID, userID, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanAgentRows(rows)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, fmt.Errorf("tenant_id required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+agentSelectCols+`
		 FROM agents
		 WHERE deleted_at IS NULL AND tenant_id = ? AND (
		     owner_id = ?
		     OR is_default = 1
		     OR id IN (SELECT agent_id FROM agent_shares WHERE user_id = ? AND tenant_id = ?)
		     OR (agent_type = 'predefined' AND id IN (
		         SELECT agent_id FROM channel_instances ci
		         WHERE ci.enabled = 1 AND ci.tenant_id = ?
		         AND EXISTS (
		             SELECT 1 FROM json_each(json_extract(ci.config, '$.allow_from'))
		             WHERE json_each.value = ?
		         )
		     ))
		 )
		 ORDER BY created_at DESC`, tid, userID, userID, tid, tid, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgentRows(rows)
}
