//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// kgUserClause returns a WHERE fragment and args for user scoping.
// If IsSharedKG is set, returns empty string (no per-user filter).
// Otherwise returns "AND user_id = ?" with the user ID.
func kgUserClause(ctx context.Context) (string, []any) {
	if store.IsSharedKG(ctx) {
		return "", nil
	}
	uid := store.UserIDFromContext(ctx)
	if uid == "" {
		return "", nil
	}
	return " AND user_id = ?", []any{uid}
}

// kgUserClauseFor is like kgUserClause but uses a given userID instead of ctx.
// Used when the userID is passed explicitly (e.g. interface methods).
func kgUserClauseFor(ctx context.Context, userID string) (string, []any) {
	if store.IsSharedKG(ctx) {
		return "", nil
	}
	if userID == "" {
		return "", nil
	}
	return " AND user_id = ?", []any{userID}
}

// scanUnixTimestamp converts a SQLite TEXT timestamp to Unix seconds (int64).
// PG stores timestamps as timestamptz and returns time.Time; sqlite stores as TEXT.
// Note: PG impl uses UnixMilli() — SQLite mirrors same convention.
func scanUnixTimestamp(src any) int64 {
	st := &sqliteTime{}
	if err := st.Scan(src); err != nil {
		return 0
	}
	return st.Time.UnixMilli()
}

// scanJSONStringMap parses a JSON object stored as TEXT into map[string]string.
// Returns empty map on nil input. Returns error on malformed JSON.
func scanJSONStringMap(data []byte) (map[string]string, error) {
	if data == nil {
		return map[string]string{}, nil
	}
	m := make(map[string]string)
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("scanJSONStringMap: %w", err)
	}
	return m, nil
}

// scanEntity scans a database row into a store.Entity.
// Column order: id, agent_id, user_id, external_id, name, entity_type, description,
//
//	properties, source_id, confidence, created_at, updated_at
func scanEntity(rows interface {
	Scan(dest ...any) error
}) (store.Entity, error) {
	var e store.Entity
	var props []byte
	var createdAt, updatedAt any
	err := rows.Scan(
		&e.ID, &e.AgentID, &e.UserID, &e.ExternalID,
		&e.Name, &e.EntityType, &e.Description,
		&props, &e.SourceID, &e.Confidence,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return e, err
	}
	e.CreatedAt = scanUnixTimestamp(createdAt)
	e.UpdatedAt = scanUnixTimestamp(updatedAt)
	if len(props) > 0 {
		p, _ := scanJSONStringMap(props)
		e.Properties = p
	}
	return e, nil
}

// scanEntityTemporal scans a database row into a store.Entity including temporal fields.
// Column order: id, agent_id, user_id, external_id, name, entity_type, description,
//
//	properties, source_id, confidence, created_at, updated_at, valid_from, valid_until
func scanEntityTemporal(rows interface {
	Scan(dest ...any) error
}) (store.Entity, error) {
	var e store.Entity
	var props []byte
	var createdAt, updatedAt any
	var validFrom, validUntil nullSqliteTime
	err := rows.Scan(
		&e.ID, &e.AgentID, &e.UserID, &e.ExternalID,
		&e.Name, &e.EntityType, &e.Description,
		&props, &e.SourceID, &e.Confidence,
		&createdAt, &updatedAt,
		&validFrom, &validUntil,
	)
	if err != nil {
		return e, err
	}
	e.CreatedAt = scanUnixTimestamp(createdAt)
	e.UpdatedAt = scanUnixTimestamp(updatedAt)
	if len(props) > 0 {
		p, _ := scanJSONStringMap(props)
		e.Properties = p
	}
	if validFrom.Valid {
		t := validFrom.Time
		e.ValidFrom = &t
	}
	if validUntil.Valid {
		t := validUntil.Time
		e.ValidUntil = &t
	}
	return e, nil
}

// scanRelation scans a database row into a store.Relation.
// Column order: id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
//
//	confidence, properties, created_at
func scanRelation(rows interface {
	Scan(dest ...any) error
}) (store.Relation, error) {
	var r store.Relation
	var props []byte
	var createdAt any
	err := rows.Scan(
		&r.ID, &r.AgentID, &r.UserID,
		&r.SourceEntityID, &r.RelationType, &r.TargetEntityID,
		&r.Confidence, &props, &createdAt,
	)
	if err != nil {
		return r, err
	}
	r.CreatedAt = scanUnixTimestamp(createdAt)
	if len(props) > 0 {
		p, _ := scanJSONStringMap(props)
		r.Properties = p
	}
	return r, nil
}
