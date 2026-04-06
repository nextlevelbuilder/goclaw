package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// MCPServerExport holds portable MCP server metadata.
// Sensitive fields (api_key, env, headers) are intentionally excluded.
type MCPServerExport struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name,omitempty"`
	Transport   string          `json:"transport"`
	Command     string          `json:"command,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
	URL         string          `json:"url,omitempty"`
	ToolPrefix  string          `json:"tool_prefix,omitempty"`
	TimeoutSec  int             `json:"timeout_sec"`
	Settings    json.RawMessage `json:"settings,omitempty"`
	Enabled     bool            `json:"enabled"`
}

// MCPGrantWithKey references an MCP agent grant by server_name and agent_key (portable).
type MCPGrantWithKey struct {
	ServerName      string          `json:"server_name"`
	AgentKey        string          `json:"agent_key"`
	Enabled         bool            `json:"enabled"`
	ToolAllow       json.RawMessage `json:"tool_allow,omitempty"`
	ToolDeny        json.RawMessage `json:"tool_deny,omitempty"`
	ConfigOverrides json.RawMessage `json:"config_overrides,omitempty"`
}

// MCPExportPreview holds lightweight counts for the export preview endpoint.
type MCPExportPreview struct {
	Servers     int `json:"servers"`
	AgentGrants int `json:"agent_grants"`
}

// ExportMCPServers returns all MCP servers scoped to the current tenant.
// Sensitive fields (api_key, headers, env) are excluded from the export.
func ExportMCPServers(ctx context.Context, db *sql.DB) ([]MCPServerExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT name, COALESCE(display_name,''), transport,"+
			" COALESCE(command,''), args, COALESCE(url,''),"+
			" COALESCE(tool_prefix,''), timeout_sec, settings, enabled"+
			" FROM mcp_servers WHERE 1=1"+tc+
			" ORDER BY name",
		tcArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MCPServerExport
	for rows.Next() {
		var (
			srv      MCPServerExport
			argsRaw  []byte
			settings []byte
		)
		if err := rows.Scan(
			&srv.Name, &srv.DisplayName, &srv.Transport,
			&srv.Command, &argsRaw, &srv.URL,
			&srv.ToolPrefix, &srv.TimeoutSec, &settings, &srv.Enabled,
		); err != nil {
			slog.Warn("mcp_export.servers.scan", "error", err)
			continue
		}
		if len(argsRaw) > 0 {
			srv.Args = json.RawMessage(argsRaw)
		}
		if len(settings) > 0 {
			srv.Settings = json.RawMessage(settings)
		}
		result = append(result, srv)
	}
	return result, rows.Err()
}

// ExportMCPGrantsWithKeys returns all MCP agent grants with server_name and agent_key resolved.
func ExportMCPGrantsWithKeys(ctx context.Context, db *sql.DB) ([]MCPGrantWithKey, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 1, "g")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT s.name, a.agent_key, g.enabled, g.tool_allow, g.tool_deny, g.config_overrides"+
			" FROM mcp_agent_grants g"+
			" JOIN mcp_servers s ON s.id = g.server_id"+
			" JOIN agents a ON a.id = g.agent_id"+
			" WHERE 1=1"+tc+
			" ORDER BY s.name, a.agent_key",
		tcArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MCPGrantWithKey
	for rows.Next() {
		var (
			g               MCPGrantWithKey
			toolAllow       []byte
			toolDeny        []byte
			configOverrides []byte
		)
		if err := rows.Scan(
			&g.ServerName, &g.AgentKey, &g.Enabled,
			&toolAllow, &toolDeny, &configOverrides,
		); err != nil {
			slog.Warn("mcp_export.grants.scan", "error", err)
			continue
		}
		if len(toolAllow) > 0 {
			g.ToolAllow = json.RawMessage(toolAllow)
		}
		if len(toolDeny) > 0 {
			g.ToolDeny = json.RawMessage(toolDeny)
		}
		if len(configOverrides) > 0 {
			g.ConfigOverrides = json.RawMessage(configOverrides)
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

// ExportMCPPreview returns aggregate counts for MCP export preview.
func ExportMCPPreview(ctx context.Context, db *sql.DB) (*MCPExportPreview, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	var p MCPExportPreview
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM mcp_servers WHERE 1=1"+tc,
		tcArgs...,
	).Scan(&p.Servers); err != nil {
		return nil, err
	}

	tc2, tcArgs2, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM mcp_agent_grants WHERE 1=1"+tc2,
		tcArgs2...,
	).Scan(&p.AgentGrants); err != nil {
		return nil, err
	}
	return &p, nil
}

// ImportMCPServer inserts an MCP server from export data if no server with that name exists.
// Returns the server UUID and whether it was newly created (false = already existed).
func ImportMCPServer(ctx context.Context, db *sql.DB, srv MCPServerExport, createdBy string) (uuid.UUID, bool, error) {
	tid := tenantIDForInsert(ctx)

	// Check if server with same name already exists
	var existing uuid.UUID
	err := db.QueryRowContext(ctx,
		"SELECT id FROM mcp_servers WHERE name = $1 AND tenant_id = $2",
		srv.Name, tid,
	).Scan(&existing)
	if err == nil {
		return existing, false, nil // already exists — return existing ID
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, err
	}

	id := uuid.Must(uuid.NewV7())
	_, err = db.ExecContext(ctx,
		`INSERT INTO mcp_servers
		   (id, name, display_name, transport, command, args, url,
		    tool_prefix, timeout_sec, settings, enabled,
		    created_by, created_at, updated_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW(),NOW(),$13)`,
		id, srv.Name, srv.DisplayName, srv.Transport,
		srv.Command, jsonOrNull(srv.Args), srv.URL,
		srv.ToolPrefix, srv.TimeoutSec, jsonOrNull(srv.Settings), srv.Enabled,
		createdBy, tid,
	)
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
}

// ImportMCPGrant upserts an MCP agent grant for an imported server.
func ImportMCPGrant(ctx context.Context, db *sql.DB, serverID, agentID uuid.UUID, g MCPGrantWithKey, grantedBy string) error {
	tid := tenantIDForInsert(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO mcp_agent_grants
		   (id, server_id, agent_id, enabled, tool_allow, tool_deny, config_overrides, granted_by, created_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW(),$9)
		 ON CONFLICT (server_id, agent_id) DO UPDATE
		   SET enabled = EXCLUDED.enabled,
		       tool_allow = EXCLUDED.tool_allow,
		       tool_deny = EXCLUDED.tool_deny,
		       config_overrides = EXCLUDED.config_overrides`,
		uuid.Must(uuid.NewV7()), serverID, agentID, g.Enabled,
		jsonOrNull(g.ToolAllow), jsonOrNull(g.ToolDeny), jsonOrNull(g.ConfigOverrides),
		grantedBy, tid,
	)
	return err
}
