package backup

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// TableDef describes a tenant-scoped table for backup/restore.
type TableDef struct {
	Name        string // table name
	Tier        int    // FK dependency tier (1=root, higher=deeper)
	HasTenantID bool   // direct tenant_id column
	ScopeColumn string // explicit filter column when not tenant_id (e.g. tenants.id)
	ParentJoin  string // JOIN clause for tables without direct tenant_id
}

// TenantTables returns all tenant-scoped tables in FK dependency order (parents first).
// Ephemeral/diagnostic tables are excluded: traces, spans, usage_snapshots,
// activity_logs, embedding_cache, pairing_requests, paired_devices,
// channel_pending_messages, cron_run_logs.
func TenantTables() []TableDef {
	return []TableDef{
		// Tier 1: root
		{Name: "tenants", Tier: 1, ScopeColumn: "id"},

		// Tier 2: direct tenant_id, no cross-table FK
		{Name: "tenant_users", Tier: 2, HasTenantID: true},
		{Name: "agents", Tier: 2, HasTenantID: true},
		{Name: "sessions", Tier: 2, HasTenantID: true},
		{Name: "api_keys", Tier: 2, HasTenantID: true},
		{Name: "config_secrets", Tier: 2, HasTenantID: true},
		{Name: "skills", Tier: 2, HasTenantID: true},
		{Name: "mcp_servers", Tier: 2, HasTenantID: true},
		{Name: "secure_cli_binaries", Tier: 2, HasTenantID: true},
		{Name: "cron_jobs", Tier: 2, HasTenantID: true},
		{Name: "channel_instances", Tier: 2, HasTenantID: true},
		{Name: "agent_teams", Tier: 2, HasTenantID: true},
		{Name: "llm_providers", Tier: 2, HasTenantID: true},

		// Tier 3: FK to Tier 2
		{Name: "agent_context_files", Tier: 3, HasTenantID: true},
		{Name: "user_context_files", Tier: 3, HasTenantID: true},
		{Name: "user_agent_profiles", Tier: 3, HasTenantID: true},
		{Name: "user_agent_overrides", Tier: 3, HasTenantID: true},
		{Name: "episodic_summaries", Tier: 3, HasTenantID: true},
		{Name: "memory_documents", Tier: 3, HasTenantID: true},
		{Name: "memory_chunks", Tier: 3, HasTenantID: true},
		{Name: "kg_entities", Tier: 3, HasTenantID: true},
		{Name: "kg_dedup_candidates", Tier: 3, HasTenantID: true},
		{Name: "vault_documents", Tier: 3, HasTenantID: true},
		{Name: "agent_evolution_metrics", Tier: 3, HasTenantID: true},
		{Name: "agent_evolution_suggestions", Tier: 3, HasTenantID: true},
		{Name: "channel_contacts", Tier: 3, HasTenantID: true},
		{Name: "subagent_tasks", Tier: 3, HasTenantID: true},
		{Name: "agent_team_members", Tier: 3, HasTenantID: true},
		{Name: "agent_links", Tier: 3, HasTenantID: true},
		{Name: "agent_shares", Tier: 3, HasTenantID: true},
		{Name: "agent_config_permissions", Tier: 3, HasTenantID: true},
		{Name: "skill_agent_grants", Tier: 3, HasTenantID: true},
		{Name: "skill_user_grants", Tier: 3, HasTenantID: true},
		{Name: "mcp_agent_grants", Tier: 3, HasTenantID: true},
		{Name: "mcp_user_grants", Tier: 3, HasTenantID: true},
		{Name: "mcp_access_requests", Tier: 3, HasTenantID: true},
		{Name: "mcp_user_credentials", Tier: 3, HasTenantID: true},
		{Name: "secure_cli_agent_grants", Tier: 3, HasTenantID: true},
		{Name: "secure_cli_user_credentials", Tier: 3, HasTenantID: true},
		{Name: "system_configs", Tier: 3, HasTenantID: true},
		{Name: "builtin_tool_tenant_configs", Tier: 3, HasTenantID: true},
		{Name: "skill_tenant_configs", Tier: 3, HasTenantID: true},

		// Tier 4: FK to Tier 3
		{Name: "kg_relations", Tier: 4, HasTenantID: true},
		{Name: "team_tasks", Tier: 4, HasTenantID: true},
		// vault_links has no tenant_id — filter via JOIN vault_documents
		{
			Name:        "vault_links",
			Tier:        4,
			HasTenantID: false,
			ParentJoin:  "vault_links vl JOIN vault_documents fd ON vl.from_doc_id = fd.id WHERE fd.tenant_id = $1",
		},

		// Tier 5
		{Name: "team_task_comments", Tier: 5, HasTenantID: true},
		{Name: "team_task_events", Tier: 5, HasTenantID: true},
		{Name: "team_task_attachments", Tier: 5, HasTenantID: true},
	}
}

func (t TableDef) tenantFilterColumn() string {
	if t.ScopeColumn != "" {
		return t.ScopeColumn
	}
	if t.HasTenantID {
		return "tenant_id"
	}
	return ""
}

func (t TableDef) exportQuery() (string, error) {
	if t.ParentJoin != "" {
		return fmt.Sprintf("SELECT vl.* FROM %s ORDER BY vl.id", t.ParentJoin), nil
	}

	column := t.tenantFilterColumn()
	if column == "" {
		return "", fmt.Errorf("table %s: no tenant filter defined", t.Name)
	}

	return fmt.Sprintf("SELECT * FROM %s WHERE %s = $1 ORDER BY id", t.Name, column), nil
}

func (t TableDef) deleteQuery() (string, error) {
	column := t.tenantFilterColumn()
	if column == "" {
		return "", fmt.Errorf("table %s: no tenant filter defined", t.Name)
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s = $1", t.Name, column), nil
}

// ExportTable writes all rows for a tenant to JSONL format (one JSON object per line).
// Uses dynamic column discovery via rows.Columns() — no hardcoded schema per table.
// Returns the number of rows written.
func ExportTable(ctx context.Context, db *sql.DB, table TableDef, tenantID uuid.UUID, w io.Writer) (int, error) {
	query, err := table.exportQuery()
	if err != nil {
		return 0, err
	}

	rows, err := db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table.Name, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns %s: %w", table.Name, err)
	}

	bw := bufio.NewWriter(w)
	count := 0

	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("scan %s: %w", table.Name, err)
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := vals[i]
			// Convert []byte to string for JSON compatibility
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[col] = v
		}

		line, err := json.Marshal(row)
		if err != nil {
			return count, fmt.Errorf("marshal %s row: %w", table.Name, err)
		}
		bw.Write(line)
		bw.WriteByte('\n')
		count++
	}

	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("rows %s: %w", table.Name, err)
	}
	return count, bw.Flush()
}

// ImportTableRows reads JSONL lines and inserts rows into tableName.
// Uses ON CONFLICT DO NOTHING for upsert-safe idempotent inserts.
// Returns the number of rows inserted.
func ImportTableRows(ctx context.Context, db *sql.DB, tableName string, reader io.Reader) (int, error) {
	sc := bufio.NewScanner(reader)
	// Allow up to 10 MB per line (large JSON rows with embedded content)
	sc.Buffer(make([]byte, 1<<20), 10<<20)

	count := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return count, fmt.Errorf("unmarshal row in %s: %w", tableName, err)
		}

		if len(row) == 0 {
			continue
		}

		cols := make([]string, 0, len(row))
		params := make([]string, 0, len(row))
		vals := make([]any, 0, len(row))

		i := 1
		for col, val := range row {
			// Validate column name to prevent SQL injection from crafted JSONL
			if !isValidColumnName(col) {
				continue
			}
			cols = append(cols, col)
			params = append(params, fmt.Sprintf("$%d", i))
			vals = append(vals, val)
			i++
		}

		q := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
			tableName,
			strings.Join(cols, ", "),
			strings.Join(params, ", "),
		)

		if _, err := db.ExecContext(ctx, q, vals...); err != nil {
			return count, fmt.Errorf("insert %s: %w", tableName, err)
		}
		count++
	}

	return count, sc.Err()
}

// isValidColumnName checks that a column name contains only safe characters.
// Prevents SQL injection from crafted JSONL column keys.
func isValidColumnName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for i, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// RemapTenantID replaces the tenant_id value in a JSON row map with newTenantID.
// Used for "new" mode restore to bind rows to the newly-created tenant.
func RemapTenantID(row map[string]any, newTenantID uuid.UUID) {
	if _, ok := row["tenant_id"]; ok {
		row["tenant_id"] = newTenantID.String()
	}
}
