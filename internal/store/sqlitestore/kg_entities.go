//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteKnowledgeGraphStore implements store.KnowledgeGraphStore for SQLite.
type SQLiteKnowledgeGraphStore struct {
	db *sql.DB
}

// NewSQLiteKnowledgeGraphStore creates a new SQLite-backed knowledge graph store.
func NewSQLiteKnowledgeGraphStore(db *sql.DB) *SQLiteKnowledgeGraphStore {
	return &SQLiteKnowledgeGraphStore{db: db}
}

// SetEmbeddingProvider is a no-op for SQLite (no vector search).
func (s *SQLiteKnowledgeGraphStore) SetEmbeddingProvider(_ store.EmbeddingProvider) {}

// Close is a no-op (db lifecycle managed externally).
func (s *SQLiteKnowledgeGraphStore) Close() error { return nil }

func (s *SQLiteKnowledgeGraphStore) UpsertEntity(ctx context.Context, entity *store.Entity) error {
	props, err := json.Marshal(entity.Properties)
	if err != nil {
		props = []byte("{}")
	}
	now := time.Now().UTC()
	id := uuid.Must(uuid.NewV7()).String()
	tid := tenantIDForInsert(ctx).String()

	sc, scArgs := kgUserClauseFor(ctx, entity.UserID)
	_ = sc // used only for reads; upsert conflict key uses explicit columns

	var actualID string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO kg_entities
			(id, agent_id, user_id, external_id, name, entity_type, description,
			 properties, source_id, confidence, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id, user_id, external_id) DO UPDATE SET
			name        = excluded.name,
			entity_type = excluded.entity_type,
			description = excluded.description,
			properties  = excluded.properties,
			source_id   = excluded.source_id,
			confidence  = excluded.confidence,
			tenant_id   = excluded.tenant_id,
			updated_at  = excluded.updated_at
		RETURNING id`,
		id, entity.AgentID, entity.UserID, entity.ExternalID,
		entity.Name, entity.EntityType, entity.Description,
		string(props), entity.SourceID, entity.Confidence, tid, now, now,
	).Scan(&actualID)
	if err != nil {
		return err
	}
	entity.ID = actualID
	_ = scArgs
	return nil
}

func (s *SQLiteKnowledgeGraphStore) GetEntity(ctx context.Context, agentID, userID, entityID string) (*store.Entity, error) {
	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)
	args := append([]any{entityID, agentID}, userArgs...)
	args = append(args, scArgs...)

	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		        properties, source_id, confidence, created_at, updated_at
		 FROM kg_entities
		 WHERE id = ? AND agent_id = ?`+userClause+sc,
		args...,
	)
	e, err := scanEntity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *SQLiteKnowledgeGraphStore) DeleteEntity(ctx context.Context, agentID, userID, entityID string) error {
	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)
	args := append([]any{entityID, agentID}, userArgs...)
	args = append(args, scArgs...)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM kg_entities WHERE id = ? AND agent_id = ?`+userClause+sc,
		args...,
	)
	return err
}

func (s *SQLiteKnowledgeGraphStore) ListEntities(ctx context.Context, agentID, userID string, opts store.EntityListOptions) ([]store.Entity, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	where := "agent_id = ? AND valid_until IS NULL"
	args := []any{agentID}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	if userClause != "" {
		where += userClause
		args = append(args, userArgs...)
	}

	if opts.EntityType != "" {
		where += " AND entity_type = ?"
		args = append(args, opts.EntityType)
	}

	sc, scArgs, _ := scopeClause(ctx)
	if sc != "" {
		where += sc
		args = append(args, scArgs...)
	}
	args = append(args, limit, opts.Offset)

	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at
		FROM kg_entities WHERE %s
		ORDER BY updated_at DESC LIMIT ? OFFSET ?`, where)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

func (s *SQLiteKnowledgeGraphStore) SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]store.Entity, error) {
	if len(query) > 500 {
		query = query[:500]
	}
	if limit <= 0 {
		limit = 20
	}

	pattern := "%" + escapeLike(query) + "%"

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)

	where := "agent_id = ? AND valid_until IS NULL AND (name || ' ' || COALESCE(description, '')) LIKE ? ESCAPE '\\'"
	args := []any{agentID, pattern}
	if userClause != "" {
		where += userClause
		args = append(args, userArgs...)
	}
	if sc != "" {
		where += sc
		args = append(args, scArgs...)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		        properties, source_id, confidence, created_at, updated_at
		 FROM kg_entities WHERE `+where+` ORDER BY updated_at DESC LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

func (s *SQLiteKnowledgeGraphStore) Stats(ctx context.Context, agentID, userID string) (*store.GraphStats, error) {
	stats := &store.GraphStats{EntityTypes: make(map[string]int)}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)

	baseWhere := "agent_id = ? AND valid_until IS NULL"
	baseArgs := []any{agentID}
	if userClause != "" {
		baseWhere += userClause
		baseArgs = append(baseArgs, userArgs...)
	}
	if sc != "" {
		baseWhere += sc
		baseArgs = append(baseArgs, scArgs...)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_entities WHERE `+baseWhere, baseArgs...,
	).Scan(&stats.EntityCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_relations WHERE `+baseWhere, baseArgs...,
	).Scan(&stats.RelationCount); err != nil {
		return nil, err
	}

	typeRows, err := s.db.QueryContext(ctx,
		`SELECT entity_type, COUNT(*) FROM kg_entities WHERE `+baseWhere+` GROUP BY entity_type`, baseArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var t string
		var c int
		if err := typeRows.Scan(&t, &c); err != nil {
			continue
		}
		stats.EntityTypes[t] = c
	}

	// Collect distinct user IDs when not filtering by specific user
	if userID == "" {
		uidArgs := append([]any{agentID}, scArgs...)
		uidRows, uidErr := s.db.QueryContext(ctx,
			`SELECT DISTINCT user_id FROM kg_entities WHERE agent_id = ?`+sc+` AND user_id != '' ORDER BY user_id`,
			uidArgs...,
		)
		if uidErr == nil {
			defer uidRows.Close()
			for uidRows.Next() {
				var uid string
				if uidRows.Scan(&uid) == nil && uid != "" {
					stats.UserIDs = append(stats.UserIDs, uid)
				}
			}
		}
	}

	return stats, nil
}

func (s *SQLiteKnowledgeGraphStore) ListEntitiesTemporal(ctx context.Context, agentID, userID string, opts store.EntityListOptions, temporal store.TemporalQueryOptions) ([]store.Entity, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	uc, ucArgs := kgUserClauseFor(ctx, userID)
	where := "agent_id = ?" + uc
	args := []any{agentID}
	args = append(args, ucArgs...)

	if opts.EntityType != "" {
		where += " AND entity_type = ?"
		args = append(args, opts.EntityType)
	}

	if !temporal.IncludeExpired {
		if temporal.AsOf != nil {
			where += " AND valid_from <= ? AND (valid_until IS NULL OR valid_until >= ?)"
			asOfStr := temporal.AsOf.UTC().Format(time.RFC3339Nano)
			args = append(args, asOfStr, asOfStr)
		} else {
			where += " AND valid_until IS NULL"
		}
	}

	sc, scArgs, _ := scopeClause(ctx)
	if sc != "" {
		where += sc
		args = append(args, scArgs...)
	}
	args = append(args, limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		        properties, source_id, confidence, created_at, updated_at, valid_from, valid_until
		 FROM kg_entities WHERE `+where+`
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list entities temporal: %w", err)
	}
	defer rows.Close()
	return scanEntityTemporalRows(rows)
}

func (s *SQLiteKnowledgeGraphStore) SupersedeEntity(ctx context.Context, old *store.Entity, replacement *store.Entity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("supersede begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	tid := tenantIDForInsert(ctx).String()

	sc, scArgs, _ := scopeClause(ctx)

	// Expire old entity
	expireArgs := append([]any{nowStr, nowStr, old.AgentID, old.UserID, old.ExternalID}, scArgs...)
	if _, err := tx.ExecContext(ctx,
		`UPDATE kg_entities SET valid_until = ?, updated_at = ?
		 WHERE agent_id = ? AND user_id = ? AND external_id = ? AND valid_until IS NULL`+sc,
		expireArgs...,
	); err != nil {
		return fmt.Errorf("supersede expire old: %w", err)
	}

	// Insert replacement with valid_from = now
	props, _ := json.Marshal(replacement.Properties)
	newID := uuid.Must(uuid.NewV7()).String()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO kg_entities
			(id, agent_id, user_id, external_id, name, entity_type, description,
			 properties, source_id, confidence, tenant_id, created_at, updated_at, valid_from)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID, replacement.AgentID, replacement.UserID, replacement.ExternalID,
		replacement.Name, replacement.EntityType, replacement.Description,
		string(props), replacement.SourceID, replacement.Confidence, tid,
		nowStr, nowStr, nowStr,
	); err != nil {
		return fmt.Errorf("supersede insert new: %w", err)
	}

	return tx.Commit()
}

// scanEntityRows scans multiple entity rows (no temporal columns).
func scanEntityRows(rows *sql.Rows) ([]store.Entity, error) {
	var result []store.Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// scanEntityTemporalRows scans multiple entity rows with temporal columns.
func scanEntityTemporalRows(rows *sql.Rows) ([]store.Entity, error) {
	var result []store.Entity
	for rows.Next() {
		e, err := scanEntityTemporal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

