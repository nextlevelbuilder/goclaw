package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListEntitiesTemporal queries entities with temporal awareness.
// AsOf=nil: current facts only (valid_until IS NULL). AsOf set: facts valid at that time.
func (s *PGKnowledgeGraphStore) ListEntitiesTemporal(ctx context.Context, agentID, userID string, opts store.EntityListOptions, temporal store.TemporalQueryOptions) ([]store.Entity, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	q := `SELECT id, agent_id, user_id, external_id, name, entity_type, description,
	             properties, source_id, confidence, created_at, updated_at, valid_from, valid_until
	      FROM kg_entities WHERE agent_id = $1 AND user_id = $2`
	args := []any{agentID, userID}
	argN := 3

	if opts.EntityType != "" {
		q += fmt.Sprintf(` AND entity_type = $%d`, argN)
		args = append(args, opts.EntityType)
		argN++
	}

	// Temporal filter
	if !temporal.IncludeExpired {
		if temporal.AsOf != nil {
			q += fmt.Sprintf(` AND valid_from <= $%d AND (valid_until IS NULL OR valid_until >= $%d)`, argN, argN)
			args = append(args, *temporal.AsOf)
			argN++
		} else {
			q += ` AND valid_until IS NULL`
		}
	}

	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, argN, argN+1)
	args = append(args, limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list entities temporal: %w", err)
	}
	defer rows.Close()

	var entities []store.Entity
	for rows.Next() {
		e, err := scanEntityTemporal(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, *e)
	}
	return entities, rows.Err()
}

// SupersedeEntity atomically expires the old entity and inserts a replacement.
func (s *PGKnowledgeGraphStore) SupersedeEntity(ctx context.Context, old *store.Entity, replacement *store.Entity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("supersede begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()

	// Expire old entity
	_, err = tx.ExecContext(ctx, `
		UPDATE kg_entities SET valid_until = $1, updated_at = $2
		WHERE agent_id = $3 AND user_id = $4 AND external_id = $5 AND valid_until IS NULL`,
		now, now.Unix(), old.AgentID, old.UserID, old.ExternalID)
	if err != nil {
		return fmt.Errorf("supersede expire old: %w", err)
	}

	// Insert replacement with valid_from = now
	props, _ := json.Marshal(replacement.Properties)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO kg_entities (id, agent_id, user_id, external_id, name, entity_type,
		    description, properties, source_id, confidence, created_at, updated_at, valid_from)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11)`,
		replacement.AgentID, replacement.UserID, replacement.ExternalID,
		replacement.Name, replacement.EntityType, replacement.Description,
		props, replacement.SourceID, replacement.Confidence, now.Unix(), now)
	if err != nil {
		return fmt.Errorf("supersede insert new: %w", err)
	}

	return tx.Commit()
}

// scanEntityTemporal scans a row including valid_from/valid_until columns.
func scanEntityTemporal(rows *sql.Rows) (*store.Entity, error) {
	var e store.Entity
	var props json.RawMessage
	err := rows.Scan(&e.ID, &e.AgentID, &e.UserID, &e.ExternalID, &e.Name,
		&e.EntityType, &e.Description, &props, &e.SourceID, &e.Confidence,
		&e.CreatedAt, &e.UpdatedAt, &e.ValidFrom, &e.ValidUntil)
	if err != nil {
		return nil, err
	}
	if props != nil {
		_ = json.Unmarshal(props, &e.Properties)
	}
	return &e, nil
}
