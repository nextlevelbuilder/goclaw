package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const partySelectCols = `id, topic, team_preset, status, mode, round, max_rounds,
    user_id, channel, chat_id, personas, context, history,
    COALESCE(summary, 'null'), artifacts, created_at, updated_at`

// PGPartyStore implements PartyStore backed by PostgreSQL.
type PGPartyStore struct {
	db *sql.DB
}

// NewPGPartyStore creates a new PGPartyStore.
func NewPGPartyStore(db *sql.DB) *PGPartyStore {
	return &PGPartyStore{db: db}
}

func (s *PGPartyStore) CreateSession(ctx context.Context, sess *store.PartySessionData) error {
	if sess.ID == uuid.Nil {
		sess.ID = store.GenNewID()
	}
	now := time.Now()
	sess.CreatedAt = now
	sess.UpdatedAt = now
	if len(sess.Personas) == 0 {
		sess.Personas = json.RawMessage("[]")
	}
	if len(sess.Context) == 0 {
		sess.Context = json.RawMessage("{}")
	}
	if len(sess.History) == 0 {
		sess.History = json.RawMessage("[]")
	}
	if len(sess.Artifacts) == 0 {
		sess.Artifacts = json.RawMessage("[]")
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO party_sessions
            (id, topic, team_preset, status, mode, round, max_rounds,
             user_id, channel, chat_id, personas, context, history, artifacts, created_at, updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		sess.ID, sess.Topic, sess.TeamPreset, sess.Status, sess.Mode,
		sess.Round, sess.MaxRounds, sess.UserID, sess.Channel, sess.ChatID,
		sess.Personas, sess.Context, sess.History, sess.Artifacts,
		sess.CreatedAt, sess.UpdatedAt)
	return err
}

func (s *PGPartyStore) GetSession(ctx context.Context, id uuid.UUID) (*store.PartySessionData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+partySelectCols+` FROM party_sessions WHERE id = $1`, id)
	return scanPartyRow(row)
}

func (s *PGPartyStore) GetActiveSession(ctx context.Context, userID, channel, chatID string) (*store.PartySessionData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+partySelectCols+` FROM party_sessions
         WHERE user_id = $1 AND channel = $2 AND chat_id = $3
           AND status IN ('assembling', 'discussing')
         ORDER BY created_at DESC LIMIT 1`, userID, channel, chatID)
	sess, err := scanPartyRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *PGPartyStore) UpdateSession(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	updates["updated_at"] = time.Now()
	return execMapUpdate(ctx, s.db, "party_sessions", id, updates)
}

func (s *PGPartyStore) ListSessions(ctx context.Context, userID string, status string, limit int) ([]*store.PartySessionData, error) {
	query := `SELECT ` + partySelectCols + ` FROM party_sessions WHERE user_id = $1`
	args := []any{userID}
	if status != "" {
		query += ` AND status = $2`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*store.PartySessionData
	for rows.Next() {
		sess, err := scanPartyRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *PGPartyStore) DeleteSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM party_sessions WHERE id = $1`, id)
	return err
}

func scanPartyRow(row *sql.Row) (*store.PartySessionData, error) {
	var s store.PartySessionData
	err := row.Scan(&s.ID, &s.Topic, &s.TeamPreset, &s.Status, &s.Mode,
		&s.Round, &s.MaxRounds, &s.UserID, &s.Channel, &s.ChatID,
		&s.Personas, &s.Context, &s.History, &s.Summary, &s.Artifacts,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func scanPartyRows(rows *sql.Rows) (*store.PartySessionData, error) {
	var s store.PartySessionData
	err := rows.Scan(&s.ID, &s.Topic, &s.TeamPreset, &s.Status, &s.Mode,
		&s.Round, &s.MaxRounds, &s.UserID, &s.Channel, &s.ChatID,
		&s.Personas, &s.Context, &s.History, &s.Summary, &s.Artifacts,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
