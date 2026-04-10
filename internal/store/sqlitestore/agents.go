//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteAgentStore implements store.AgentStore backed by SQLite.
type SQLiteAgentStore struct {
	db *sql.DB
	// embProvider is intentionally omitted — vector search not supported in SQLite lite edition.
}

func NewSQLiteAgentStore(db *sql.DB) *SQLiteAgentStore {
	return &SQLiteAgentStore{db: db}
}

// SetEmbeddingProvider is a no-op for SQLite — vector not supported.
func (s *SQLiteAgentStore) SetEmbeddingProvider(_ store.EmbeddingProvider) {}

// agentSelectCols is the column list for all agent SELECT queries.
const agentSelectCols = `id, agent_key, display_name, frontmatter, owner_id, provider, model,
	 context_window, max_tool_iterations, workspace, restrict_to_workspace,
	 tools_config, sandbox_config, subagents_config, memory_config,
	 compaction_config, context_pruning, other_config,
	 emoji, agent_description, thinking_level, max_tokens,
	 self_evolve, skill_evolve, skill_nudge_interval,
	 reasoning_config, workspace_sharing, chatgpt_oauth_routing,
	 shell_deny_groups, kg_dedup_config,
	 agent_type, is_default, status, budget_monthly_cents, created_at, updated_at, tenant_id`

func (s *SQLiteAgentStore) Create(ctx context.Context, agent *store.AgentData) error {
	if agent.ID == uuid.Nil {
		agent.ID = store.GenNewID()
	}
	now := time.Now()
	agent.CreatedAt = now
	agent.UpdatedAt = now
	tenantID := agent.TenantID
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agents (id, agent_key, display_name, frontmatter, owner_id, provider, model,
		 context_window, max_tool_iterations, workspace, restrict_to_workspace,
		 tools_config, sandbox_config, subagents_config, memory_config,
		 compaction_config, context_pruning, other_config,
		 emoji, agent_description, thinking_level, max_tokens,
		 self_evolve, skill_evolve, skill_nudge_interval,
		 reasoning_config, workspace_sharing, chatgpt_oauth_routing,
		 shell_deny_groups, kg_dedup_config,
		 agent_type, is_default, status, budget_monthly_cents, created_at, updated_at, tenant_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		agent.ID, agent.AgentKey,
		agent.DisplayName,
		sql.NullString{String: agent.Frontmatter, Valid: agent.Frontmatter != ""},
		agent.OwnerID, agent.Provider, agent.Model,
		agent.ContextWindow, agent.MaxToolIterations, agent.Workspace, agent.RestrictToWorkspace,
		jsonOrEmpty(agent.ToolsConfig), jsonOrNull(agent.SandboxConfig), jsonOrNull(agent.SubagentsConfig), jsonOrNull(agent.MemoryConfig),
		jsonOrNull(agent.CompactionConfig), jsonOrNull(agent.ContextPruning), jsonOrEmpty(agent.OtherConfig),
		agent.Emoji, agent.AgentDescription, agent.ThinkingLevel, agent.MaxTokens,
		agent.SelfEvolve, agent.SkillEvolve, agent.SkillNudgeInterval,
		jsonOrEmpty(agent.ReasoningConfig), jsonOrEmpty(agent.WorkspaceSharing), jsonOrEmpty(agent.ChatGPTOAuthRouting),
		jsonOrEmpty(agent.ShellDenyGroups), jsonOrEmpty(agent.KGDedupConfig),
		agent.AgentType, agent.IsDefault, agent.Status, agent.BudgetMonthlyCents,
		now, now, tenantID,
	)
	return err
}

func (s *SQLiteAgentStore) GetByKey(ctx context.Context, agentKey string) (*store.AgentData, error) {
	var row *sql.Row
	if store.IsCrossTenant(ctx) {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+` FROM agents WHERE agent_key = ? AND deleted_at IS NULL`, agentKey)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("agent not found: %s", agentKey)
		}
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+` FROM agents WHERE agent_key = ? AND deleted_at IS NULL AND tenant_id = ?`,
			agentKey, tid)
	}
	d, err := scanAgentRow(row)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", agentKey)
	}
	return d, nil
}

func (s *SQLiteAgentStore) GetByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	var row *sql.Row
	if store.IsCrossTenant(ctx) {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+` FROM agents WHERE id = ? AND deleted_at IS NULL`, id)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("agent not found: %s", id)
		}
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+` FROM agents WHERE id = ? AND deleted_at IS NULL AND tenant_id = ?`, id, tid)
	}
	d, err := scanAgentRow(row)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", id)
	}
	return d, nil
}

func (s *SQLiteAgentStore) GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+agentSelectCols+` FROM agents WHERE id = ? AND deleted_at IS NULL`, id)
	d, err := scanAgentRow(row)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", id)
	}
	return d, nil
}

func (s *SQLiteAgentStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// Coerce NOT NULL int columns: null → default to prevent constraint violations.
	if v, ok := updates["skill_nudge_interval"]; ok && v == nil {
		updates["skill_nudge_interval"] = 0
	}

	// Unset existing default before setting a new one (scoped to same tenant).
	if v, ok := updates["is_default"]; ok {
		if isDefault, _ := v.(bool); isDefault {
			if store.IsCrossTenant(ctx) {
				if _, err := s.db.ExecContext(ctx,
					"UPDATE agents SET is_default = 0 WHERE is_default = 1 AND id != ? AND deleted_at IS NULL", id); err != nil {
					slog.Warn("agents.unset_default", "error", err)
				}
			} else {
				tid := store.TenantIDFromContext(ctx)
				if tid != uuid.Nil {
					if _, err := s.db.ExecContext(ctx,
						"UPDATE agents SET is_default = 0 WHERE is_default = 1 AND id != ? AND deleted_at IS NULL AND tenant_id = ?",
						id, tid); err != nil {
						slog.Warn("agents.unset_default", "error", err)
					}
				}
			}
		}
	}

	updates["updated_at"] = time.Now()
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "agents", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("agent not found: %s", id)
	}
	return execMapUpdateWhereTenant(ctx, s.db, "agents", updates, id, tid)
}

func (s *SQLiteAgentStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE id = ?", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("agent not found: %s", id)
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE id = ? AND tenant_id = ?", id, tid)
	return err
}

func (s *SQLiteAgentStore) List(ctx context.Context, ownerID string) ([]store.AgentData, error) {
	q := `SELECT ` + agentSelectCols + ` FROM agents WHERE deleted_at IS NULL`
	var args []any

	if ownerID != "" {
		q += " AND owner_id = ?"
		args = append(args, ownerID)
	}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND tenant_id = ?"
			args = append(args, tid)
		}
	}

	q += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgentRows(rows)
}

func (s *SQLiteAgentStore) GetDefault(ctx context.Context) (*store.AgentData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents WHERE deleted_at IS NULL
			 ORDER BY is_default DESC, created_at ASC LIMIT 1`)
		return scanAgentRow(row)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, fmt.Errorf("agent not found: default")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+agentSelectCols+`
		 FROM agents WHERE deleted_at IS NULL AND tenant_id = ?
		 ORDER BY is_default DESC, created_at ASC LIMIT 1`, tid)
	return scanAgentRow(row)
}

// --- Scan helpers ---

type agentRowScanner interface {
	Scan(dest ...any) error
}

func scanAgentRow(row agentRowScanner) (*store.AgentData, error) {
	var d store.AgentData
	var frontmatter sql.NullString
	var toolsCfg, sandboxCfg, subagentsCfg, memoryCfg, compactionCfg, pruningCfg, otherCfg *[]byte
	var reasoningCfg, wsCfg, oauthCfg, shellCfg, kgCfg *[]byte
	createdAt, updatedAt := scanTimePair()
	err := row.Scan(
		&d.ID, &d.AgentKey, &d.DisplayName, &frontmatter, &d.OwnerID, &d.Provider, &d.Model,
		&d.ContextWindow, &d.MaxToolIterations, &d.Workspace, &d.RestrictToWorkspace,
		&toolsCfg, &sandboxCfg, &subagentsCfg, &memoryCfg, &compactionCfg, &pruningCfg, &otherCfg,
		&d.Emoji, &d.AgentDescription, &d.ThinkingLevel, &d.MaxTokens,
		&d.SelfEvolve, &d.SkillEvolve, &d.SkillNudgeInterval,
		&reasoningCfg, &wsCfg, &oauthCfg, &shellCfg, &kgCfg,
		&d.AgentType, &d.IsDefault, &d.Status, &d.BudgetMonthlyCents,
		createdAt, updatedAt, &d.TenantID,
	)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = createdAt.Time
	d.UpdatedAt = updatedAt.Time
	if frontmatter.Valid {
		d.Frontmatter = frontmatter.String
	}
	if toolsCfg != nil {
		d.ToolsConfig = *toolsCfg
	}
	if sandboxCfg != nil {
		d.SandboxConfig = *sandboxCfg
	}
	if subagentsCfg != nil {
		d.SubagentsConfig = *subagentsCfg
	}
	if memoryCfg != nil {
		d.MemoryConfig = *memoryCfg
	}
	if compactionCfg != nil {
		d.CompactionConfig = *compactionCfg
	}
	if pruningCfg != nil {
		d.ContextPruning = *pruningCfg
	}
	if otherCfg != nil {
		d.OtherConfig = *otherCfg
	}
	if reasoningCfg != nil {
		d.ReasoningConfig = *reasoningCfg
	}
	if wsCfg != nil {
		d.WorkspaceSharing = *wsCfg
	}
	if oauthCfg != nil {
		d.ChatGPTOAuthRouting = *oauthCfg
	}
	if shellCfg != nil {
		d.ShellDenyGroups = *shellCfg
	}
	if kgCfg != nil {
		d.KGDedupConfig = *kgCfg
	}
	return &d, nil
}

func scanAgentRows(rows *sql.Rows) ([]store.AgentData, error) {
	var result []store.AgentData
	for rows.Next() {
		d, err := scanAgentRow(rows)
		if err != nil {
			continue
		}
		result = append(result, *d)
	}
	return result, rows.Err()
}
