package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGHookStore implements hooks.HookStore backed by PostgreSQL.
type PGHookStore struct {
	db *sql.DB

	cacheMu sync.Mutex
	cache   map[string]pgHookCacheEntry // keyed by resolveKey
}

type pgHookCacheEntry struct {
	result     []hooks.HookConfig
	maxVersion int
	expiresAt  time.Time
}

const hookCacheTTL = 5 * time.Second

// NewPGHookStore returns a PGHookStore backed by the given *sql.DB.
func NewPGHookStore(db *sql.DB) *PGHookStore {
	return &PGHookStore{
		db:    db,
		cache: make(map[string]pgHookCacheEntry),
	}
}

// ─── Create ─────────────────────────────────────────────────────────────────

func (s *PGHookStore) Create(ctx context.Context, cfg hooks.HookConfig) (uuid.UUID, error) {
	// Honor a caller-provided fixed ID when non-nil (H9 fix) — the Phase 04
	// builtin seeder needs idempotent UPSERTs keyed by UUIDv5, and tests need
	// deterministic IDs. Fall back to UUIDv7 only when the caller did not
	// supply one.
	id := cfg.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	now := time.Now().UTC()

	cfgJSON, err := json.Marshal(cfg.Config)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal config: %w", err)
	}
	metaJSON, err := json.Marshal(cfg.Metadata)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal metadata: %w", err)
	}

	tid := cfg.TenantID
	if tid == uuid.Nil {
		tid = tenantIDForInsert(ctx)
	}

	var matcher, ifExpr, name *string
	if cfg.Matcher != "" {
		matcher = &cfg.Matcher
	}
	if cfg.IfExpr != "" {
		ifExpr = &cfg.IfExpr
	}
	if cfg.Name != "" {
		name = &cfg.Name
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO hooks
		  (id, tenant_id, scope, event, handler_type,
		   config, matcher, if_expr, timeout_ms, on_timeout,
		   priority, enabled, version, source, metadata, name, created_by,
		   created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$14,$15,$16,$17,$17)`,
		id, tid, string(cfg.Scope), string(cfg.Event), string(cfg.HandlerType),
		cfgJSON, matcher, ifExpr, cfg.TimeoutMS, string(cfg.OnTimeout),
		cfg.Priority, cfg.Enabled, string(cfg.Source), metaJSON, name, cfg.CreatedBy,
		now,
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert hook: %w", err)
	}
	// Bridge deprecated AgentID → AgentIDs for junction insertion.
	agentIDs := cfg.AgentIDs
	if len(agentIDs) == 0 && cfg.AgentID != nil && *cfg.AgentID != uuid.Nil {
		agentIDs = []uuid.UUID{*cfg.AgentID}
	}
	for _, aid := range agentIDs {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO hook_agents (hook_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			id, aid); err != nil {
			return uuid.Nil, fmt.Errorf("insert hook agent link: %w", err)
		}
	}
	s.invalidateCache()
	return id, nil
}

// ─── GetByID ─────────────────────────────────────────────────────────────────

func (s *PGHookStore) GetByID(ctx context.Context, id uuid.UUID) (*hooks.HookConfig, error) {
	q := `
		SELECT id, tenant_id, scope, event, handler_type,
		       config, matcher, if_expr, timeout_ms, on_timeout,
		       priority, enabled, version, source, metadata, name, created_by,
		       created_at, updated_at
		FROM hooks WHERE id = $1`
	args := []any{id}

	// Tenant-scope guard: non-master callers only see their own rows + globals.
	// Global hooks use tenant_id = SentinelTenantID (store.MasterTenantID).
	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required for non-master scope")
		}
		q += " AND (tenant_id = $2 OR tenant_id = $3)"
		args = append(args, tid, store.MasterTenantID)
	}

	row := s.db.QueryRowContext(ctx, q, args...)
	cfg, err := scanHookPGRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get hook by id: %w", err)
	}
	if aids, err := s.GetHookAgents(ctx, cfg.ID); err == nil {
		cfg.AgentIDs = aids
	}
	return cfg, nil
}

// ─── List ────────────────────────────────────────────────────────────────────

func (s *PGHookStore) List(ctx context.Context, filter hooks.ListFilter) ([]hooks.HookConfig, error) {
	q := `SELECT id, tenant_id, scope, event, handler_type,
		       config, matcher, if_expr, timeout_ms, on_timeout,
		       priority, enabled, version, source, metadata, name, created_by,
		       created_at, updated_at FROM hooks WHERE 1=1`
	var args []any
	n := 1

	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required for non-master scope")
		}
		// Return tenant-specific + global hooks.
		q += fmt.Sprintf(" AND (tenant_id = $%d OR tenant_id = $%d)", n, n+1)
		args = append(args, tid, store.MasterTenantID)
		n += 2
	} else if filter.TenantID != nil {
		q += fmt.Sprintf(" AND tenant_id = $%d", n)
		args = append(args, *filter.TenantID)
		n++
	}

	if filter.AgentID != nil {
		q += fmt.Sprintf(" AND id IN (SELECT hook_id FROM hook_agents WHERE agent_id = $%d)", n)
		args = append(args, *filter.AgentID)
		n++
	}
	if filter.Event != nil {
		q += fmt.Sprintf(" AND event = $%d", n)
		args = append(args, string(*filter.Event))
		n++
	}
	if filter.Scope != nil {
		q += fmt.Sprintf(" AND scope = $%d", n)
		args = append(args, string(*filter.Scope))
		n++
	}
	if filter.Enabled != nil {
		q += fmt.Sprintf(" AND enabled = $%d", n)
		args = append(args, *filter.Enabled)
		n++
	}
	_ = n
	q += " ORDER BY priority DESC, created_at ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	defer rows.Close()

	var result []hooks.HookConfig
	for rows.Next() {
		cfg, err := scanHookPGRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Batch-populate AgentIDs from junction table.
	for i := range result {
		if aids, err := s.GetHookAgents(ctx, result[i].ID); err == nil {
			result[i].AgentIDs = aids
		}
	}
	return result, nil
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (s *PGHookStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if _, ok := updates["version"]; ok {
		return fmt.Errorf("callers must not include 'version' in update map")
	}

	// Builtin-row protection (Phase 04). Rows with source='builtin' are canonical
	// embed-seeded artifacts: UI/user writes may only toggle `enabled`. The
	// builtin seeder itself passes through via hooks.WithSeedBypass ctx.
	// Fail-closed: a transient GetByID error must not let a non-enabled patch
	// slip through against a builtin row.
	if !hooks.IsSeedBypass(ctx) {
		current, err := s.GetByID(ctx, id)
		if err != nil {
			return fmt.Errorf("builtin guard: %w", err)
		}
		if current != nil && current.Source == hooks.SourceBuiltin {
			for k := range updates {
				if k != "enabled" {
					return hooks.ErrBuiltinReadOnly
				}
			}
		}
	}

	// Handle agent_ids separately — routes to junction table, not a column.
	if raw, ok := updates["agent_ids"]; ok {
		delete(updates, "agent_ids")
		ids, err := parseAgentIDsFromAny(raw)
		if err != nil {
			return fmt.Errorf("invalid agent_ids: %w", err)
		}
		if err := s.SetHookAgents(ctx, id, ids); err != nil {
			return err
		}
	}

	// Marshal map/slice values to JSON for JSONB columns.
	for k, v := range updates {
		switch k {
		case "config", "metadata":
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("marshal %s: %w", k, err)
			}
			updates[k] = json.RawMessage(b)
		}
	}

	// Build SET clause with version bump.
	var setClauses []string
	var args []any
	i := 1
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	// Always bump version and updated_at atomically.
	setClauses = append(setClauses, fmt.Sprintf("version = version + 1, updated_at = $%d", i))
	args = append(args, time.Now().UTC())
	i++

	args = append(args, id)
	q := fmt.Sprintf("UPDATE hooks SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), i)
	i++

	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required for non-master scope")
		}
		q += fmt.Sprintf(" AND tenant_id = $%d", i)
		args = append(args, tid)
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("hook not found: %s", id)
	}
	s.invalidateCache()
	return nil
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func (s *PGHookStore) Delete(ctx context.Context, id uuid.UUID) error {
	// Builtin-row protection (Phase 04): reject unless caller marked seed-bypass.
	// Fail-closed on GetByID errors so we don't accidentally delete a builtin.
	if !hooks.IsSeedBypass(ctx) {
		current, err := s.GetByID(ctx, id)
		if err != nil {
			return fmt.Errorf("builtin guard: %w", err)
		}
		if current != nil && current.Source == hooks.SourceBuiltin {
			return hooks.ErrBuiltinReadOnly
		}
	}

	q := "DELETE FROM hooks WHERE id = $1"
	args := []any{id}

	if !store.IsMasterScope(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required for non-master scope")
		}
		q += " AND tenant_id = $2"
		args = append(args, tid)
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("hook not found: %s", id)
	}
	s.invalidateCache()
	return nil
}

// ─── ResolveForEvent ─────────────────────────────────────────────────────────

func (s *PGHookStore) ResolveForEvent(ctx context.Context, event hooks.Event) ([]hooks.HookConfig, error) {
	tenantID := event.TenantID
	agentID := event.AgentID
	hookEvent := event.HookEvent

	// Check max version in DB to validate cache freshness.
	var maxVersion int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0) FROM hooks
		 WHERE enabled = TRUE AND event = $1
		   AND (tenant_id = $2 OR tenant_id = $3)
		   AND (
		     scope IN ('global', 'tenant')
		     OR (scope = 'agent' AND EXISTS (
		       SELECT 1 FROM hook_agents aha
		       WHERE aha.hook_id = hooks.id AND aha.agent_id = $4
		     ))
		   )`,
		string(hookEvent), tenantID, store.MasterTenantID, agentID,
	).Scan(&maxVersion)
	if err != nil {
		return nil, fmt.Errorf("resolve version check: %w", err)
	}

	key := hookResolveKey(tenantID, agentID, hookEvent)
	s.cacheMu.Lock()
	entry, ok := s.cache[key]
	s.cacheMu.Unlock()

	if ok && time.Now().Before(entry.expiresAt) && entry.maxVersion == maxVersion {
		return entry.result, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, scope, event, handler_type,
		       config, matcher, if_expr, timeout_ms, on_timeout,
		       priority, enabled, version, source, metadata, name, created_by,
		       created_at, updated_at
		FROM hooks
		WHERE enabled = TRUE AND event = $1
		  AND (tenant_id = $2 OR tenant_id = $3)
		  AND (
		    scope IN ('global', 'tenant')
		    OR (scope = 'agent' AND EXISTS (
		      SELECT 1 FROM hook_agents aha
		      WHERE aha.hook_id = hooks.id AND aha.agent_id = $4
		    ))
		  )
		ORDER BY priority DESC, created_at ASC`,
		string(hookEvent), tenantID, store.MasterTenantID, agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: %w", err)
	}
	defer rows.Close()

	var result []hooks.HookConfig
	for rows.Next() {
		cfg, err := scanHookPGRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.cacheMu.Lock()
	s.cache[key] = pgHookCacheEntry{
		result:     result,
		maxVersion: maxVersion,
		expiresAt:  time.Now().Add(hookCacheTTL),
	}
	s.cacheMu.Unlock()

	return result, nil
}

// ─── WriteExecution ──────────────────────────────────────────────────────────

func (s *PGHookStore) WriteExecution(ctx context.Context, exec hooks.HookExecution) error {
	metaJSON, err := json.Marshal(exec.Metadata)
	if err != nil {
		return fmt.Errorf("marshal exec metadata: %w", err)
	}

	now := exec.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var dedupKey, sessionID, inputHash, errStr *string
	if exec.DedupKey != "" {
		dedupKey = &exec.DedupKey
	}
	if exec.SessionID != "" {
		sessionID = &exec.SessionID
	}
	if exec.InputHash != "" {
		inputHash = &exec.InputHash
	}
	if exec.Error != "" {
		errStr = &exec.Error
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO hook_executions
		  (id, hook_id, session_id, event, input_hash, decision,
		   duration_ms, retry, dedup_key, error, error_detail, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING`,
		exec.ID, exec.HookID, sessionID, string(exec.Event),
		inputHash, string(exec.Decision),
		exec.DurationMS, exec.Retry, dedupKey,
		errStr, exec.ErrorDetail, metaJSON, now,
	)
	if err != nil {
		return fmt.Errorf("write execution: %w", err)
	}
	return nil
}

// parseAgentIDsFromAny converts the raw JSON-decoded value (typically
// []interface{} of string UUIDs) into []uuid.UUID.
func parseAgentIDsFromAny(raw any) ([]uuid.UUID, error) {
	switch v := raw.(type) {
	case []any:
		var ids []uuid.UUID
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("invalid agent_id %q: %w", s, err)
			}
			ids = append(ids, id)
		}
		return ids, nil
	case []uuid.UUID:
		return v, nil
	default:
		return nil, fmt.Errorf("agent_ids: unsupported type %T", raw)
	}
}

// ─── N:M junction: hook_agents ────────────────────────────────────────

func (s *PGHookStore) SetHookAgents(ctx context.Context, hookID uuid.UUID, agentIDs []uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM hook_agents WHERE hook_id = $1", hookID); err != nil {
		return fmt.Errorf("clear hook agents: %w", err)
	}
	for _, aid := range agentIDs {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO hook_agents (hook_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			hookID, aid); err != nil {
			return fmt.Errorf("insert hook agent: %w", err)
		}
	}
	s.invalidateCache()
	return tx.Commit()
}

func (s *PGHookStore) GetHookAgents(ctx context.Context, hookID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT agent_id FROM hook_agents WHERE hook_id = $1", hookID)
	if err != nil {
		return nil, fmt.Errorf("get hook agents: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ─── cache helpers ───────────────────────────────────────────────────────────

func (s *PGHookStore) invalidateCache() {
	s.cacheMu.Lock()
	s.cache = make(map[string]pgHookCacheEntry)
	s.cacheMu.Unlock()
}

func hookResolveKey(tenantID, agentID uuid.UUID, event hooks.HookEvent) string {
	return tenantID.String() + "|" + agentID.String() + "|" + string(event)
}

// ─── scan helper ─────────────────────────────────────────────────────────────

type pgRowScanner interface {
	Scan(dest ...any) error
}

func scanHookPGRow(row pgRowScanner) (*hooks.HookConfig, error) {
	var (
		cfg                    hooks.HookConfig
		createdBy              *uuid.UUID
		scope, event           string
		handlerType, onTimeout string
		source                 string
		matcher, ifExpr, name  sql.NullString
		cfgJSON, metaJSON      []byte
	)
	err := row.Scan(
		&cfg.ID, &cfg.TenantID,
		&scope, &event, &handlerType,
		&cfgJSON, &matcher, &ifExpr,
		&cfg.TimeoutMS, &onTimeout,
		&cfg.Priority, &cfg.Enabled, &cfg.Version,
		&source, &metaJSON, &name, &createdBy,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	cfg.Scope = hooks.Scope(scope)
	cfg.Event = hooks.HookEvent(event)
	cfg.HandlerType = hooks.HandlerType(handlerType)
	cfg.OnTimeout = hooks.Decision(onTimeout)
	cfg.Source = source
	cfg.CreatedBy = createdBy

	if matcher.Valid {
		cfg.Matcher = matcher.String
	}
	if ifExpr.Valid {
		cfg.IfExpr = ifExpr.String
	}
	if name.Valid {
		cfg.Name = name.String
	}

	if len(cfgJSON) > 0 {
		_ = json.Unmarshal(cfgJSON, &cfg.Config)
	}
	if cfg.Config == nil {
		cfg.Config = map[string]any{}
	}
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &cfg.Metadata)
	}
	if cfg.Metadata == nil {
		cfg.Metadata = map[string]any{}
	}

	return &cfg, nil
}
