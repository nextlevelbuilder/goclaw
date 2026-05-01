package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGAgentStore implements store.AgentStore backed by Postgres.
type PGAgentStore struct {
	db          *sql.DB
	embProvider store.EmbeddingProvider // optional: for agent frontmatter embeddings
}

func NewPGAgentStore(db *sql.DB) *PGAgentStore {
	return &PGAgentStore{db: db}
}

// SetEmbeddingProvider sets the embedding provider for agent frontmatter vectors.
func (s *PGAgentStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.embProvider = provider
}

// generateAgentEmbedding creates an embedding for an agent's displayName+frontmatter and stores it.
func (s *PGAgentStore) generateAgentEmbedding(ctx context.Context, agentID uuid.UUID, displayName, frontmatter string) {
	if s.embProvider == nil || frontmatter == "" {
		return
	}
	text := displayName
	if frontmatter != "" {
		text += ": " + frontmatter
	}
	embeddings, err := s.embProvider.Embed(ctx, []string{text})
	if err != nil || len(embeddings) == 0 || len(embeddings[0]) == 0 {
		slog.Warn("agent embedding generation failed", "agent", agentID, "error", err)
		return
	}
	vecStr := vectorToString(embeddings[0])
	if _, err := s.db.ExecContext(ctx, `UPDATE agents SET embedding = $1::vector WHERE id = $2`, vecStr, agentID); err != nil {
		slog.Warn("agent embedding update failed", "agent", agentID, "error", err)
	}
}

// BackfillAgentEmbeddings generates embeddings for all active agents that have frontmatter but no embedding.
func (s *PGAgentStore) BackfillAgentEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, nil
	}
	var pending []agentBackfillRow
	if err := pkgSqlxDB.SelectContext(ctx, &pending,
		`SELECT id, COALESCE(display_name, '') AS display_name, COALESCE(frontmatter, '') AS frontmatter
		 FROM agents WHERE deleted_at IS NULL AND frontmatter IS NOT NULL AND frontmatter != '' AND embedding IS NULL`,
	); err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	slog.Info("backfilling agent embeddings", "count", len(pending))
	updated := 0
	for _, ag := range pending {
		text := ag.DisplayName
		if ag.Frontmatter != "" {
			text += ": " + ag.Frontmatter
		}
		embeddings, err := s.embProvider.Embed(ctx, []string{text})
		if err != nil || len(embeddings) == 0 || len(embeddings[0]) == 0 {
			continue
		}
		vecStr := vectorToString(embeddings[0])
		if _, err := s.db.ExecContext(ctx, `UPDATE agents SET embedding = $1::vector WHERE id = $2`, vecStr, ag.ID); err != nil {
			continue
		}
		updated++
	}
	slog.Info("agent embeddings backfill complete", "updated", updated)
	return updated, nil
}

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

func (s *PGAgentStore) Create(ctx context.Context, agent *store.AgentData) error {
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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
		         $19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37)`,
		agent.ID, agent.AgentKey, agent.DisplayName, sql.NullString{String: agent.Frontmatter, Valid: agent.Frontmatter != ""}, agent.OwnerID, agent.Provider, agent.Model,
		agent.ContextWindow, agent.MaxToolIterations, agent.Workspace, agent.RestrictToWorkspace,
		jsonOrEmpty(agent.ToolsConfig), jsonOrNull(agent.SandboxConfig), jsonOrNull(agent.SubagentsConfig), jsonOrNull(agent.MemoryConfig),
		jsonOrNull(agent.CompactionConfig), jsonOrNull(agent.ContextPruning), jsonOrEmpty(agent.OtherConfig),
		agent.Emoji, agent.AgentDescription, agent.ThinkingLevel, agent.MaxTokens,
		agent.SelfEvolve, agent.SkillEvolve, agent.SkillNudgeInterval,
		jsonOrEmpty(agent.ReasoningConfig), jsonOrEmpty(agent.WorkspaceSharing), jsonOrEmpty(agent.ChatGPTOAuthRouting),
		jsonOrEmpty(agent.ShellDenyGroups), jsonOrEmpty(agent.KGDedupConfig),
		agent.AgentType, agent.IsDefault, agent.Status, agent.BudgetMonthlyCents, now, now, tenantID,
	)
	if err != nil {
		return err
	}

	// Generate embedding for new agent with frontmatter
	if agent.Frontmatter != "" && s.embProvider != nil {
		go s.generateAgentEmbedding(context.Background(), agent.ID, agent.DisplayName, agent.Frontmatter)
	}
	return nil
}

func (s *PGAgentStore) GetByKey(ctx context.Context, agentKey string) (*store.AgentData, error) {
	var row *sql.Row
	if store.IsCrossTenant(ctx) {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents WHERE agent_key = $1 AND deleted_at IS NULL`, agentKey)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("agent not found: %s", agentKey)
		}
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents WHERE agent_key = $1 AND deleted_at IS NULL AND tenant_id = $2`, agentKey, tid)
	}
	d, err := scanAgentRow(row)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", agentKey)
	}
	return d, nil
}

func (s *PGAgentStore) GetByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	var row *sql.Row
	if store.IsCrossTenant(ctx) {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents WHERE id = $1 AND deleted_at IS NULL`, id)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("agent not found: %s", id)
		}
		row = s.db.QueryRowContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents WHERE id = $1 AND deleted_at IS NULL AND tenant_id = $2`, id, tid)
	}
	d, err := scanAgentRow(row)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", id)
	}
	return d, nil
}

func (s *PGAgentStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// Coerce NOT NULL columns: null → default to prevent constraint violations.
	// Promoted TEXT columns (migration 000037): null → empty string.
	for _, col := range []string{"emoji", "agent_description", "thinking_level"} {
		if v, ok := updates[col]; ok && v == nil {
			updates[col] = ""
		}
	}
	// Promoted INT/BOOL columns: null → 0/false.
	for _, col := range []string{"skill_nudge_interval", "max_tokens", "self_evolve", "skill_evolve", "is_default"} {
		if v, ok := updates[col]; ok && v == nil {
			if col == "self_evolve" || col == "skill_evolve" || col == "is_default" {
				updates[col] = false
			} else {
				updates[col] = 0
			}
		}
	}
	// NOT NULL JSONB columns: null → empty object.
	for _, col := range []string{"other_config", "tools_config", "chatgpt_oauth_routing", "reasoning_config", "workspace_sharing", "shell_deny_groups", "kg_dedup_config"} {
		if v, ok := updates[col]; ok && v == nil {
			updates[col] = []byte("{}")
		}
	}

	// If setting this agent as default, unset any existing default first (scoped to same tenant).
	if v, ok := updates["is_default"]; ok {
		if isDefault, _ := v.(bool); isDefault {
			if store.IsCrossTenant(ctx) {
				if _, err := s.db.ExecContext(ctx,
					"UPDATE agents SET is_default = false WHERE is_default = true AND id != $1 AND deleted_at IS NULL", id); err != nil {
					slog.Warn("agents.unset_default", "error", err)
				}
			} else {
				tid := store.TenantIDFromContext(ctx)
				if tid != uuid.Nil {
					if _, err := s.db.ExecContext(ctx,
						"UPDATE agents SET is_default = false WHERE is_default = true AND id != $1 AND deleted_at IS NULL AND tenant_id = $2", id, tid); err != nil {
						slog.Warn("agents.unset_default", "error", err)
					}
				}
			}
		}
	}

	updates["updated_at"] = time.Now()
	if store.IsCrossTenant(ctx) {
		if err := execMapUpdateWhere(ctx, s.db, "agents", updates, "id = $IDX AND deleted_at IS NULL", id); err != nil {
			return err
		}
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("agent not found: %s", id)
		}
		if err := execMapUpdateWhereTenant(ctx, s.db, "agents", updates, id, tid); err != nil {
			return err
		}
	}

	// Regenerate embedding when frontmatter changes
	if _, hasFrontmatter := updates["frontmatter"]; hasFrontmatter && s.embProvider != nil {
		bgCtx := store.WithTenantID(context.Background(), store.TenantIDFromContext(ctx))
		go func() {
			ag, agErr := s.GetByID(bgCtx, id)
			if agErr == nil {
				s.generateAgentEmbedding(bgCtx, id, ag.DisplayName, ag.Frontmatter)
			}
		}()
	}
	return nil
}

func (s *PGAgentStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE id = $1", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("agent not found: %s", id)
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE id = $1 AND tenant_id = $2", id, tid)
	return err
}

func (s *PGAgentStore) List(ctx context.Context, ownerID string) ([]store.AgentData, error) {
	q := `SELECT ` + agentSelectCols + ` FROM agents WHERE deleted_at IS NULL`
	var args []any
	argIdx := 1

	if ownerID != "" {
		q += fmt.Sprintf(" AND owner_id = $%d", argIdx)
		args = append(args, ownerID)
		argIdx++
	}

	if clause, targs, _, err := scopeClause(ctx, argIdx); err == nil && clause != "" {
		q += clause
		args = append(args, targs...)
		argIdx++
	}

	q += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgentRows(rows)
}

func (s *PGAgentStore) GetDefault(ctx context.Context) (*store.AgentData, error) {
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
		 FROM agents WHERE deleted_at IS NULL AND tenant_id = $1
		 ORDER BY is_default DESC, created_at ASC LIMIT 1`, tid)
	return scanAgentRow(row)
}

// --- Access Control ---

func (s *PGAgentStore) ShareAgent(ctx context.Context, agentID uuid.UUID, userID, role, grantedBy string) error {
	if err := store.ValidateUserID(userID); err != nil {
		return err
	}
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_shares (id, agent_id, user_id, role, granted_by, tenant_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (agent_id, user_id) DO UPDATE SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by`,
		store.GenNewID(), agentID, userID, role, grantedBy, tid, time.Now(),
	)
	return err
}

func (s *PGAgentStore) RevokeShare(ctx context.Context, agentID uuid.UUID, userID string) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx,
			"DELETE FROM agent_shares WHERE agent_id = $1 AND user_id = $2", agentID, userID)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM agent_shares WHERE agent_id = $1 AND user_id = $2 AND tenant_id = $3", agentID, userID, tid)
	return err
}

func (s *PGAgentStore) ListShares(ctx context.Context, agentID uuid.UUID) ([]store.AgentShareData, error) {
	q := "SELECT id, agent_id, user_id, role, granted_by, created_at FROM agent_shares WHERE agent_id = $1"
	args := []any{agentID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = $2"
		args = append(args, tid)
	}
	var rows []agentShareRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	result := make([]store.AgentShareData, len(rows))
	for i, r := range rows {
		result[i] = r.toAgentShareData()
	}
	return result, nil
}

func (s *PGAgentStore) CanAccess(ctx context.Context, agentID uuid.UUID, userID string) (bool, string, error) {
	// Check ownership + default flag
	var ownerID string
	var isDefault bool
	var err error
	if store.IsCrossTenant(ctx) {
		err = s.db.QueryRowContext(ctx,
			"SELECT owner_id, is_default FROM agents WHERE id = $1 AND deleted_at IS NULL", agentID,
		).Scan(&ownerID, &isDefault)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, "", fmt.Errorf("agent not found")
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT owner_id, is_default FROM agents WHERE id = $1 AND deleted_at IS NULL AND tenant_id = $2", agentID, tid,
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
			"SELECT role FROM agent_shares WHERE agent_id = $1 AND user_id = $2", agentID, userID,
		).Scan(&role)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, "", nil
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT role FROM agent_shares WHERE agent_id = $1 AND user_id = $2 AND tenant_id = $3", agentID, userID, tid,
		).Scan(&role)
	}
	if err != nil {
		return false, "", nil
	}
	return true, role, nil
}

func (s *PGAgentStore) ListAccessible(ctx context.Context, userID string) ([]store.AgentData, error) {
	if store.IsCrossTenant(ctx) {
		rows, err := s.db.QueryContext(ctx,
			`SELECT `+agentSelectCols+`
			 FROM agents
			 WHERE deleted_at IS NULL AND (
			     owner_id = $1
			     OR is_default = true
			     OR id IN (SELECT agent_id FROM agent_shares WHERE user_id = $1)
			     OR (
			         agent_type = 'predefined'
			         AND id IN (
			             SELECT agent_id FROM channel_instances ci
			             WHERE ci.enabled = true
			             AND EXISTS (
			                 SELECT 1 FROM jsonb_array_elements_text(ci.config->'allow_from') af
			                 WHERE af = $1
			             )
			         )
			     )
			 )
			 ORDER BY created_at DESC`, userID)
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
		 WHERE deleted_at IS NULL AND tenant_id = $2 AND (
		     owner_id = $1
		     OR is_default = true
		     OR id IN (SELECT agent_id FROM agent_shares WHERE user_id = $1 AND tenant_id = $2)
		     OR (
		         agent_type = 'predefined'
		         AND id IN (
		             SELECT agent_id FROM channel_instances ci
		             WHERE ci.enabled = true AND ci.tenant_id = $2
		             AND EXISTS (
		                 SELECT 1 FROM jsonb_array_elements_text(ci.config->'allow_from') af
		                 WHERE af = $1
		             )
		         )
		     )
		 )
		 ORDER BY created_at DESC`, userID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgentRows(rows)
}

// --- Scan helpers ---

type agentRowScanner interface {
	Scan(dest ...any) error
}

func scanAgentRow(row agentRowScanner) (*store.AgentData, error) {
	var d store.AgentData
	var frontmatter sql.NullString
	// pgx: scan nullable JSONB into *[]byte (NOT *json.RawMessage — pgx can't scan NULL into defined types)
	var toolsCfg, sandboxCfg, subagentsCfg, memoryCfg, compactionCfg, pruningCfg, otherCfg *[]byte
	var reasoningCfg, wsCfg, oauthCfg, shellCfg, kgCfg *[]byte
	err := row.Scan(&d.ID, &d.AgentKey, &d.DisplayName, &frontmatter, &d.OwnerID, &d.Provider, &d.Model,
		&d.ContextWindow, &d.MaxToolIterations, &d.Workspace, &d.RestrictToWorkspace,
		&toolsCfg, &sandboxCfg, &subagentsCfg, &memoryCfg, &compactionCfg, &pruningCfg, &otherCfg,
		&d.Emoji, &d.AgentDescription, &d.ThinkingLevel, &d.MaxTokens,
		&d.SelfEvolve, &d.SkillEvolve, &d.SkillNudgeInterval,
		&reasoningCfg, &wsCfg, &oauthCfg, &shellCfg, &kgCfg,
		&d.AgentType, &d.IsDefault, &d.Status, &d.BudgetMonthlyCents, &d.CreatedAt, &d.UpdatedAt, &d.TenantID)
	if err != nil {
		return nil, err
	}
	if frontmatter.Valid {
		d.Frontmatter = frontmatter.String
	}
	// Convert *[]byte → json.RawMessage (nil-safe)
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
	return result, nil
}

// execMapUpdateWhere is like execMapUpdate but with a custom WHERE clause.
// The whereClause should use $IDX as placeholder for the ID (will be replaced with the next arg index).
// Column names are validated against a strict identifier regex to prevent SQL injection.
func execMapUpdateWhere(ctx context.Context, db *sql.DB, table string, updates map[string]any, whereClause string, id uuid.UUID) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	i := 1
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			slog.Warn("security.invalid_column_name", "table", table, "column", col)
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	args = append(args, id)
	// Replace $IDX with the actual parameter index for the id
	where := fmt.Sprintf(whereClause[0:0]+"%s", whereClause)
	finalWhere := ""
	for _, ch := range where {
		finalWhere += string(ch)
	}
	// Simple replace: $IDX → $N
	idxStr := fmt.Sprintf("$%d", i)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		table,
		joinStrings(setClauses, ", "),
		replaceIDX(whereClause, idxStr))
	_, err := db.ExecContext(ctx, q, args...)
	return err
}

func joinStrings(s []string, sep string) string {
	var result strings.Builder
	for i, v := range s {
		if i > 0 {
			result.WriteString(sep)
		}
		result.WriteString(v)
	}
	return result.String()
}

// ResetStuckSummoning flips rows with status='summoning' to 'summon_failed'.
// Called at startup to recover from crashes where summon goroutines died mid-flight.
func (s *PGAgentStore) ResetStuckSummoning(ctx context.Context) (int64, error) {
	const q = `UPDATE agents SET status = $1 WHERE status = $2`
	res, err := s.db.ExecContext(ctx, q, store.AgentStatusSummonFailed, store.AgentStatusSummoning)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func replaceIDX(s, replacement string) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		if i+4 <= len(s) && s[i:i+4] == "$IDX" {
			result.WriteString(replacement)
			i += 3
		} else {
			result.WriteString(string(s[i]))
		}
	}
	return result.String()
}

