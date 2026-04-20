package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListEntitiesTemporal queries entities with temporal awareness.
// AsOf=nil: current facts only (valid_until IS NULL). AsOf set: facts valid at that time.
func (s *PGKnowledgeGraphStore) ListEntitiesTemporal(ctx context.Context, agentID, userID string, opts store.EntityListOptions, temporal store.TemporalQueryOptions) ([]store.Entity, error) {
	aid := parseUUIDOrNil(agentID)
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	where := "agent_id = $1 AND valid_until IS NULL"
	args := []any{aid}
	argN := 2

	userWhere, userArgs := kgUserWhere(ctx, userID, argN)
	if userWhere != "" {
		where += userWhere
		args = append(args, userArgs...)
		argN += len(userArgs)
	}

	if opts.EntityType != "" {
		where += fmt.Sprintf(` AND entity_type = $%d`, argN)
		args = append(args, opts.EntityType)
		argN++
	}

	// Temporal filter
	if !temporal.IncludeExpired {
		if temporal.AsOf != nil {
			where += fmt.Sprintf(` AND valid_from <= $%d AND (valid_until IS NULL OR valid_until >= $%d)`, argN, argN)
			args = append(args, *temporal.AsOf)
			argN++
		}
		// default: valid_until IS NULL already in base where
	}

	// Tenant scope
	tc, tcArgs, _, err := scopeClause(ctx, argN)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		argN += len(tcArgs)
	}

	args = append(args, limit, opts.Offset)
	q := fmt.Sprintf(`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		             properties, source_id, confidence, created_at, updated_at, valid_from, valid_until, event_time
		      FROM kg_entities WHERE %s
		      ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argN, argN+1)

	var tRows []entityTemporalRow
	if err := pkgSqlxDB.SelectContext(ctx, &tRows, q, args...); err != nil {
		return nil, fmt.Errorf("list entities temporal: %w", err)
	}
	entities := make([]store.Entity, len(tRows))
	for i := range tRows {
		entities[i] = tRows[i].toEntity()
	}
	return entities, nil
}

// SupersedeEntity atomically expires the old entity and inserts a replacement.
func (s *PGKnowledgeGraphStore) SupersedeEntity(ctx context.Context, old *store.Entity, replacement *store.Entity) error {
	aid, err := parseUUID(old.AgentID)
	if err != nil {
		return fmt.Errorf("kg supersede entity: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("supersede begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	tid := tenantIDForInsert(ctx)

	// Tenant scope for UPDATE
	tc, tcArgs, _, err := scopeClause(ctx, 6)
	if err != nil {
		return err
	}

	// Expire old entity
	expireArgs := append([]any{now, now, aid, old.UserID, old.ExternalID}, tcArgs...)
	_, err = tx.ExecContext(ctx, `
		UPDATE kg_entities SET valid_until = $1, updated_at = $2
		WHERE agent_id = $3 AND user_id = $4 AND external_id = $5 AND valid_until IS NULL`+tc,
		expireArgs...)
	if err != nil {
		return fmt.Errorf("supersede expire old: %w", err)
	}

	// Insert replacement with valid_from = now
	props, _ := json.Marshal(replacement.Properties)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO kg_entities (id, agent_id, user_id, external_id, name, entity_type,
		    description, properties, source_id, confidence, tenant_id, created_at, updated_at, valid_from)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11, $12)`,
		aid, replacement.UserID, replacement.ExternalID,
		replacement.Name, replacement.EntityType, replacement.Description,
		props, replacement.SourceID, replacement.Confidence, tid, now, now)
	if err != nil {
		return fmt.Errorf("supersede insert new: %w", err)
	}

	return tx.Commit()
}

// SearchEntitiesByEventTime returns entities whose event_time falls within [fromTime, toTime].
// Either bound may be nil for open-ended ranges. Only returns entities with non-NULL event_time.
func (s *PGKnowledgeGraphStore) SearchEntitiesByEventTime(ctx context.Context, agentID, userID string, fromTime, toTime *time.Time, limit int) ([]store.Entity, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg search by event time: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}

	where := "agent_id = $1 AND valid_until IS NULL AND event_time IS NOT NULL"
	args := []any{aid}
	argN := 2

	userWhere, userArgs := kgUserWhere(ctx, userID, argN)
	if userWhere != "" {
		where += userWhere
		args = append(args, userArgs...)
		argN += len(userArgs)
	}
	if fromTime != nil {
		where += fmt.Sprintf(" AND event_time >= $%d", argN)
		args = append(args, *fromTime)
		argN++
	}
	if toTime != nil {
		where += fmt.Sprintf(" AND event_time <= $%d", argN)
		args = append(args, *toTime)
		argN++
	}

	tc, tcArgs, _, err := scopeClause(ctx, argN)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		argN += len(tcArgs)
	}

	args = append(args, limit)
	q := fmt.Sprintf(`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		             properties, source_id, confidence, created_at, updated_at, valid_from, valid_until, event_time
		      FROM kg_entities WHERE %s ORDER BY event_time DESC LIMIT $%d`, where, argN)

	var tRows []entityTemporalRow
	if err := pkgSqlxDB.SelectContext(ctx, &tRows, q, args...); err != nil {
		return nil, fmt.Errorf("search entities by event time: %w", err)
	}
	entities := make([]store.Entity, len(tRows))
	for i := range tRows {
		entities[i] = tRows[i].toEntity()
	}
	return entities, nil
}
