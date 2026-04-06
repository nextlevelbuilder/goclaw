package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGEvolutionSuggestionStore implements store.EvolutionSuggestionStore backed by PostgreSQL.
type PGEvolutionSuggestionStore struct {
	db *sql.DB
}

// NewPGEvolutionSuggestionStore creates a new PG-backed evolution suggestion store.
func NewPGEvolutionSuggestionStore(db *sql.DB) *PGEvolutionSuggestionStore {
	return &PGEvolutionSuggestionStore{db: db}
}

func (s *PGEvolutionSuggestionStore) CreateSuggestion(ctx context.Context, sg store.EvolutionSuggestion) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = sg.TenantID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_evolution_suggestions
		 (id, tenant_id, agent_id, suggestion_type, suggestion, rationale, parameters, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sg.ID, tenantID, sg.AgentID, sg.SuggestionType, sg.Suggestion,
		sg.Rationale, sg.Parameters, sg.Status)
	return err
}

func (s *PGEvolutionSuggestionStore) ListSuggestions(ctx context.Context, agentID uuid.UUID, status string, limit int) ([]store.EvolutionSuggestion, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, tenant_id, agent_id, suggestion_type, suggestion, rationale,
	                 parameters, status, reviewed_by, reviewed_at, created_at
	          FROM agent_evolution_suggestions
	          WHERE agent_id = $1 AND tenant_id = $2`
	args := []any{agentID, tenantID}
	if status != "" {
		query += " AND status = $3"
		args = append(args, status)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []store.EvolutionSuggestion
	for rows.Next() {
		var sg store.EvolutionSuggestion
		if err := rows.Scan(&sg.ID, &sg.TenantID, &sg.AgentID, &sg.SuggestionType,
			&sg.Suggestion, &sg.Rationale, &sg.Parameters, &sg.Status,
			&sg.ReviewedBy, &sg.ReviewedAt, &sg.CreatedAt); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, sg)
	}
	return suggestions, rows.Err()
}

func (s *PGEvolutionSuggestionStore) UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status, reviewedBy string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_evolution_suggestions
		 SET status = $1, reviewed_by = $2, reviewed_at = $3
		 WHERE id = $4`,
		status, reviewedBy, now, id)
	return err
}

func (s *PGEvolutionSuggestionStore) GetSuggestion(ctx context.Context, id uuid.UUID) (*store.EvolutionSuggestion, error) {
	var sg store.EvolutionSuggestion
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, agent_id, suggestion_type, suggestion, rationale,
		        parameters, status, reviewed_by, reviewed_at, created_at
		 FROM agent_evolution_suggestions WHERE id = $1`, id).Scan(
		&sg.ID, &sg.TenantID, &sg.AgentID, &sg.SuggestionType,
		&sg.Suggestion, &sg.Rationale, &sg.Parameters, &sg.Status,
		&sg.ReviewedBy, &sg.ReviewedAt, &sg.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sg, nil
}

