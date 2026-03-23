package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- Agent Grants ---

func (s *PGMCPServerStore) GrantToAgent(ctx context.Context, g *store.MCPAgentGrant) error {
	if err := store.ValidateUserID(g.GrantedBy); err != nil {
		return err
	}
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	g.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, tool_allow, tool_deny, config_overrides, granted_by, created_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (server_id, agent_id) DO UPDATE SET
		   enabled = EXCLUDED.enabled, tool_allow = EXCLUDED.tool_allow,
		   tool_deny = EXCLUDED.tool_deny, config_overrides = EXCLUDED.config_overrides,
		   granted_by = EXCLUDED.granted_by`,
		g.ID, g.ServerID, g.AgentID, g.Enabled,
		jsonOrNull(g.ToolAllow), jsonOrNull(g.ToolDeny), jsonOrNull(g.ConfigOverrides),
		g.GrantedBy, g.CreatedAt, tenantIDForInsert(ctx),
	)
	return err
}

func (s *PGMCPServerStore) RevokeFromAgent(ctx context.Context, serverID, agentID uuid.UUID) error {
	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM mcp_agent_grants WHERE server_id = $1 AND agent_id = $2"+tClause,
		append([]any{serverID, agentID}, tArgs...)...)
	return err
}

func (s *PGMCPServerStore) ListAgentGrants(ctx context.Context, agentID uuid.UUID) ([]store.MCPAgentGrant, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, agent_id, enabled, tool_allow, tool_deny, config_overrides, granted_by, created_at
		 FROM mcp_agent_grants WHERE agent_id = $1`+tClause,
		append([]any{agentID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.MCPAgentGrant
	for rows.Next() {
		var g store.MCPAgentGrant
		if err := rows.Scan(&g.ID, &g.ServerID, &g.AgentID, &g.Enabled,
			&g.ToolAllow, &g.ToolDeny, &g.ConfigOverrides, &g.GrantedBy, &g.CreatedAt); err != nil {
			continue
		}
		result = append(result, g)
	}
	return result, nil
}

func (s *PGMCPServerStore) ListServerGrants(ctx context.Context, serverID uuid.UUID) ([]store.MCPAgentGrant, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, agent_id, enabled,
		 COALESCE(tool_allow, '[]'::jsonb), COALESCE(tool_deny, '[]'::jsonb),
		 COALESCE(config_overrides, '{}'::jsonb), granted_by, created_at
		 FROM mcp_agent_grants WHERE server_id = $1`+tClause+` ORDER BY created_at`,
		append([]any{serverID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.MCPAgentGrant, 0)
	for rows.Next() {
		var g store.MCPAgentGrant
		if err := rows.Scan(&g.ID, &g.ServerID, &g.AgentID, &g.Enabled,
			&g.ToolAllow, &g.ToolDeny, &g.ConfigOverrides, &g.GrantedBy, &g.CreatedAt); err != nil {
			continue
		}
		result = append(result, g)
	}
	return result, nil
}

// --- Counts ---

func (s *PGMCPServerStore) CountAgentGrantsByServer(ctx context.Context) (map[uuid.UUID]int, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 1)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT server_id, COUNT(*) FROM mcp_agent_grants WHERE 1=1`+tClause+` GROUP BY server_id`,
		tArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int)
	for rows.Next() {
		var serverID uuid.UUID
		var count int
		if err := rows.Scan(&serverID, &count); err != nil {
			continue
		}
		result[serverID] = count
	}
	return result, nil
}

// --- User Grants ---

func (s *PGMCPServerStore) GrantToUser(ctx context.Context, g *store.MCPUserGrant) error {
	if err := store.ValidateUserID(g.UserID); err != nil {
		return err
	}
	if err := store.ValidateUserID(g.GrantedBy); err != nil {
		return err
	}
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	g.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_user_grants (id, server_id, user_id, enabled, tool_allow, tool_deny, granted_by, created_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (server_id, user_id) DO UPDATE SET
		   enabled = EXCLUDED.enabled, tool_allow = EXCLUDED.tool_allow,
		   tool_deny = EXCLUDED.tool_deny, granted_by = EXCLUDED.granted_by`,
		g.ID, g.ServerID, g.UserID, g.Enabled,
		jsonOrNull(g.ToolAllow), jsonOrNull(g.ToolDeny),
		g.GrantedBy, g.CreatedAt, tenantIDForInsert(ctx),
	)
	return err
}

func (s *PGMCPServerStore) RevokeFromUser(ctx context.Context, serverID uuid.UUID, userID string) error {
	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM mcp_user_grants WHERE server_id = $1 AND user_id = $2"+tClause,
		append([]any{serverID, userID}, tArgs...)...)
	return err
}

// --- Resolution ---

func (s *PGMCPServerStore) ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]store.MCPAccessInfo, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ms.id, ms.name, ms.display_name, ms.transport, ms.command, ms.args, ms.url, ms.headers, ms.env,
		 ms.api_key, ms.tool_prefix, ms.timeout_sec, ms.settings, ms.enabled, ms.created_by, ms.created_at, ms.updated_at,
		 mag.tool_allow, mag.tool_deny
		 FROM mcp_servers ms
		 INNER JOIN mcp_agent_grants mag ON ms.id = mag.server_id AND mag.agent_id = $1 AND mag.enabled = true
		 LEFT JOIN mcp_user_grants mug ON ms.id = mug.server_id AND mug.user_id = $2
		 WHERE ms.enabled = true
		   AND (mug.id IS NULL OR mug.enabled = true)`+
			strings.Replace(tClause, "tenant_id", "ms.tenant_id", 1),
		append([]any{agentID, userID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.MCPAccessInfo, 0)
	for rows.Next() {
		var srv store.MCPServerData
		var displayName, command, url, apiKey, toolPrefix *string
		var args, headers, env *[]byte
		var toolAllowJSON, toolDenyJSON *[]byte

		if err := rows.Scan(
			&srv.ID, &srv.Name, &displayName, &srv.Transport, &command,
			&args, &url, &headers, &env,
			&apiKey, &toolPrefix, &srv.TimeoutSec,
			&srv.Settings, &srv.Enabled, &srv.CreatedBy, &srv.CreatedAt, &srv.UpdatedAt,
			&toolAllowJSON, &toolDenyJSON,
		); err != nil {
			continue
		}
		srv.DisplayName = derefStr(displayName)
		srv.Command = derefStr(command)
		srv.URL = derefStr(url)
		srv.ToolPrefix = derefStr(toolPrefix)
		srv.Args = derefBytes(args)
		srv.Headers = s.decryptJSONB(derefBytes(headers))
		srv.Env = s.decryptJSONB(derefBytes(env))
		if apiKey != nil && *apiKey != "" && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(*apiKey, s.encKey); err == nil {
				srv.APIKey = decrypted
			}
		} else {
			srv.APIKey = derefStr(apiKey)
		}

		info := store.MCPAccessInfo{Server: srv}
		if toolAllowJSON != nil {
			json.Unmarshal(*toolAllowJSON, &info.ToolAllow)
		}
		if toolDenyJSON != nil {
			json.Unmarshal(*toolDenyJSON, &info.ToolDeny)
		}
		result = append(result, info)
	}
	return result, nil
}

// --- Access Requests ---

func (s *PGMCPServerStore) CreateRequest(ctx context.Context, req *store.MCPAccessRequest) error {
	if err := store.ValidateUserID(req.RequestedBy); err != nil {
		return err
	}
	if req.ID == uuid.Nil {
		req.ID = store.GenNewID()
	}
	req.Status = "pending"
	req.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, reason, tool_allow, requested_by, created_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		req.ID, req.ServerID, nilUUID(req.AgentID), nilStr(req.UserID),
		req.Scope, req.Status, nilStr(req.Reason),
		jsonOrNull(req.ToolAllow), req.RequestedBy, req.CreatedAt, tenantIDForInsert(ctx),
	)
	return err
}

func (s *PGMCPServerStore) ListPendingRequests(ctx context.Context) ([]store.MCPAccessRequest, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 1)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, agent_id, user_id, scope, status, reason, tool_allow, requested_by,
		 reviewed_by, reviewed_at, review_note, created_at
		 FROM mcp_access_requests WHERE status = 'pending'`+tClause+` ORDER BY created_at`,
		tArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.MCPAccessRequest
	for rows.Next() {
		var r store.MCPAccessRequest
		var agentID *uuid.UUID
		var userID, reviewedBy, reviewNote *string
		if err := rows.Scan(&r.ID, &r.ServerID, &agentID, &userID, &r.Scope, &r.Status,
			&r.Reason, &r.ToolAllow, &r.RequestedBy,
			&reviewedBy, &r.ReviewedAt, &reviewNote, &r.CreatedAt); err != nil {
			continue
		}
		r.AgentID = agentID
		r.UserID = derefStr(userID)
		r.ReviewedBy = derefStr(reviewedBy)
		r.ReviewNote = derefStr(reviewNote)
		result = append(result, r)
	}
	return result, nil
}

func (s *PGMCPServerStore) ReviewRequest(ctx context.Context, requestID uuid.UUID, approved bool, reviewedBy, note string) error {
	if err := store.ValidateUserID(reviewedBy); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Load the request
	var req store.MCPAccessRequest
	var agentID *uuid.UUID
	var userID *string
	tClause, tArgs, err2 := tenantClauseN(ctx, 2)
	if err2 != nil {
		return err2
	}
	err = tx.QueryRowContext(ctx,
		`SELECT id, server_id, agent_id, user_id, scope, status, tool_allow
		 FROM mcp_access_requests WHERE id = $1 AND status = 'pending'`+tClause,
		append([]any{requestID}, tArgs...)...,
	).Scan(&req.ID, &req.ServerID, &agentID, &userID, &req.Scope, &req.Status, &req.ToolAllow)
	if err != nil {
		return fmt.Errorf("request not found or not pending: %w", err)
	}

	status := "rejected"
	if approved {
		status = "approved"
	}
	now := time.Now()

	// Update request status
	_, err = tx.ExecContext(ctx,
		`UPDATE mcp_access_requests SET status = $1, reviewed_by = $2, reviewed_at = $3, review_note = $4 WHERE id = $5`,
		status, reviewedBy, now, nilStr(note), requestID,
	)
	if err != nil {
		return err
	}

	// If approved, insert the grant
	if approved {
		switch req.Scope {
		case "agent":
			if agentID == nil {
				return fmt.Errorf("agent_id required for agent scope")
			}
			_, err = tx.ExecContext(ctx,
				`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, tool_allow, granted_by, created_at, tenant_id)
				 VALUES ($1,$2,$3,true,$4,$5,$6,$7)
				 ON CONFLICT (server_id, agent_id) DO UPDATE SET enabled = true, tool_allow = EXCLUDED.tool_allow, granted_by = EXCLUDED.granted_by`,
				store.GenNewID(), req.ServerID, *agentID, jsonOrNull(req.ToolAllow), reviewedBy, now, tenantIDForInsert(ctx),
			)
		case "user":
			if userID == nil || *userID == "" {
				return fmt.Errorf("user_id required for user scope")
			}
			_, err = tx.ExecContext(ctx,
				`INSERT INTO mcp_user_grants (id, server_id, user_id, enabled, tool_allow, granted_by, created_at, tenant_id)
				 VALUES ($1,$2,$3,true,$4,$5,$6,$7)
				 ON CONFLICT (server_id, user_id) DO UPDATE SET enabled = true, tool_allow = EXCLUDED.tool_allow, granted_by = EXCLUDED.granted_by`,
				store.GenNewID(), req.ServerID, *userID, jsonOrNull(req.ToolAllow), reviewedBy, now, tenantIDForInsert(ctx),
			)
		default:
			return fmt.Errorf("unknown scope: %s", req.Scope)
		}
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
