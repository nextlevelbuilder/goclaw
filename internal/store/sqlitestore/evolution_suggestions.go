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

// SQLiteEvolutionSuggestionStore implements store.EvolutionSuggestionStore backed by SQLite.
type SQLiteEvolutionSuggestionStore struct {
	db *sql.DB
}

// NewSQLiteEvolutionSuggestionStore creates a new SQLite-backed evolution suggestion store.
func NewSQLiteEvolutionSuggestionStore(db *sql.DB) *SQLiteEvolutionSuggestionStore {
	return &SQLiteEvolutionSuggestionStore{db: db}
}

func (s *SQLiteEvolutionSuggestionStore) CreateSuggestion(ctx context.Context, sg store.EvolutionSuggestion) error {
	tenantID := tenantIDForInsert(ctx)
	if tenantID == uuid.Nil {
		return fmt.Errorf("evolution.CreateSuggestion: tenant_id required in context")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_evolution_suggestions
		 (id, tenant_id, agent_id, suggestion_type, suggestion, rationale, parameters, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sg.ID.String(), tenantID.String(), sg.AgentID.String(),
		string(sg.SuggestionType), sg.Suggestion, sg.Rationale,
		string(sg.Parameters), sg.Status)
	return err
}

func (s *SQLiteEvolutionSuggestionStore) ListSuggestions(ctx context.Context, agentID uuid.UUID, status string, limit int) ([]store.EvolutionSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, tenant_id, agent_id, suggestion_type, suggestion, rationale,
	                 parameters, status, reviewed_by, reviewed_at, created_at
	          FROM agent_evolution_suggestions
	          WHERE agent_id = ? AND tenant_id = ?`
	args := []any{agentID.String(), tenantID.String()}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []store.EvolutionSuggestion
	for rows.Next() {
		var sg store.EvolutionSuggestion
		var idStr, tenantStr, agentStr string
		var paramsBytes []byte
		var reviewedBy sql.NullString
		var reviewedAt nullSqliteTime
		var createdAt sqliteTime
		if err := rows.Scan(&idStr, &tenantStr, &agentStr, &sg.SuggestionType,
			&sg.Suggestion, &sg.Rationale, &paramsBytes, &sg.Status,
			&reviewedBy, &reviewedAt, &createdAt); err != nil {
			return nil, err
		}
		sg.ID, _ = uuid.Parse(idStr)
		sg.TenantID, _ = uuid.Parse(tenantStr)
		sg.AgentID, _ = uuid.Parse(agentStr)
		sg.Parameters = paramsBytes
		sg.ReviewedBy = reviewedBy.String
		if reviewedAt.Valid {
			t := reviewedAt.Time
			sg.ReviewedAt = &t
		}
		sg.CreatedAt = createdAt.Time
		suggestions = append(suggestions, sg)
	}
	return suggestions, rows.Err()
}

func (s *SQLiteEvolutionSuggestionStore) UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status, reviewedBy string) error {
	tenantID := tenantIDForInsert(ctx)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_evolution_suggestions
		 SET status = ?, reviewed_by = ?, reviewed_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		status, reviewedBy, now, id.String(), tenantID.String())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("suggestion not found or access denied")
	}
	return nil
}

func (s *SQLiteEvolutionSuggestionStore) UpdateSuggestionParameters(ctx context.Context, id uuid.UUID, params json.RawMessage) error {
	tenantID := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_evolution_suggestions SET parameters = ?
		 WHERE id = ? AND tenant_id = ?`,
		string(params), id.String(), tenantID.String())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("suggestion not found or access denied")
	}
	return nil
}

func (s *SQLiteEvolutionSuggestionStore) GetSuggestion(ctx context.Context, id uuid.UUID) (*store.EvolutionSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	var sg store.EvolutionSuggestion
	var idStr, tenantStr, agentStr string
	var paramsBytes []byte
	var reviewedBy sql.NullString
	var reviewedAt nullSqliteTime
	var createdAt sqliteTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, agent_id, suggestion_type, suggestion, rationale,
		        parameters, status, reviewed_by, reviewed_at, created_at
		 FROM agent_evolution_suggestions WHERE id = ? AND tenant_id = ?`,
		id.String(), tenantID.String()).Scan(
		&idStr, &tenantStr, &agentStr, &sg.SuggestionType,
		&sg.Suggestion, &sg.Rationale, &paramsBytes, &sg.Status,
		&reviewedBy, &reviewedAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sg.ID, _ = uuid.Parse(idStr)
	sg.TenantID, _ = uuid.Parse(tenantStr)
	sg.AgentID, _ = uuid.Parse(agentStr)
	sg.Parameters = paramsBytes
	sg.ReviewedBy = reviewedBy.String
	if reviewedAt.Valid {
		t := reviewedAt.Time
		sg.ReviewedAt = &t
	}
	sg.CreatedAt = createdAt.Time
	return &sg, nil
}

// Ensure SQLiteEvolutionSuggestionStore implements store.EvolutionSuggestionStore.
var _ store.EvolutionSuggestionStore = (*SQLiteEvolutionSuggestionStore)(nil)
