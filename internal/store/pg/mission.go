package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGMissionStore implements store.MissionStore backed by PostgreSQL. Goals,
// Milestones, and Acceptance are stored as text[] columns; Metadata is JSONB.
type PGMissionStore struct {
	db *sql.DB
}

// NewPGMissionStore creates a PG-backed mission store.
func NewPGMissionStore(db *sql.DB) *PGMissionStore {
	return &PGMissionStore{db: db}
}

// CreateMission inserts a new mission. Status defaults to active; the tenant
// comes from the context tenant (master fallback).
func (s *PGMissionStore) CreateMission(ctx context.Context, m *store.Mission) error {
	if m.ID == uuid.Nil {
		m.ID = store.GenNewID()
	}
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if m.Status == "" {
		m.Status = store.MissionStatusActive
	}
	if !store.ValidMissionStatus(m.Status) {
		return fmt.Errorf("create mission: unknown status %q", m.Status)
	}
	m.TenantID = tenantIDForInsert(ctx)

	// goals/milestones/acceptance are NOT NULL columns; normalize nil slices to
	// the empty array literal "{}" (pqStringArray returns nil only for nil input).
	if m.Goals == nil {
		m.Goals = []string{}
	}
	if m.Milestones == nil {
		m.Milestones = []string{}
	}
	if m.Acceptance == nil {
		m.Acceptance = []string{}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO missions (id, tenant_id, name, goals, milestones, acceptance, status, agent_id, session_key, checkpoint_seq, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13)`,
		m.ID, m.TenantID, m.Name, pqStringArray(m.Goals), pqStringArray(m.Milestones),
		pqStringArray(m.Acceptance), m.Status, nilUUID(m.AgentID), m.SessionKey,
		m.CheckpointSeq, missionMetadata(m.Metadata), m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create mission: insert: %w", err)
	}
	return nil
}

// GetMission returns one mission by id, scoped to the context tenant.
func (s *PGMissionStore) GetMission(ctx context.Context, id uuid.UUID) (*store.Mission, error) {
	where, args := buildMissionGetWhere(ctx, id)
	q := `SELECT id, tenant_id, name, goals, milestones, acceptance, status, agent_id, session_key, checkpoint_seq, metadata::text AS metadata, created_at, updated_at
		 FROM missions` + where
	var row missionRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrMissionNotFound
		}
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListMissions returns mission rows filtered by opts, scoped to the context
// tenant. Newest first.
func (s *PGMissionStore) ListMissions(ctx context.Context, opts store.MissionListOpts) ([]store.Mission, error) {
	where, args := buildMissionListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, name, goals, milestones, acceptance, status, agent_id, session_key, checkpoint_seq, metadata::text AS metadata, created_at, updated_at
		 FROM missions` + where +
		` ORDER BY created_at DESC, id DESC` +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []missionRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.Mission, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

// UpdateMissionStatus transitions one mission's status, scoped to the context
// tenant. The target status must be a known status.
func (s *PGMissionStore) UpdateMissionStatus(ctx context.Context, id uuid.UUID, status string) error {
	if !store.ValidMissionStatus(status) {
		return fmt.Errorf("update mission status: unknown status %q", status)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE missions SET status = $3, updated_at = NOW() WHERE id = $1 AND tenant_id = $2`,
		id, tid, status)
	if err != nil {
		return fmt.Errorf("update mission status: %w", err)
	}
	return nil
}

// UpdateMissionProgress links the mission to the latest durable checkpoint
// snapshot seq, scoped to the context tenant.
func (s *PGMissionStore) UpdateMissionProgress(ctx context.Context, id uuid.UUID, checkpointSeq int) error {
	if checkpointSeq < 0 {
		return fmt.Errorf("update mission progress: negative checkpoint seq %d", checkpointSeq)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE missions SET checkpoint_seq = $3, updated_at = NOW() WHERE id = $1 AND tenant_id = $2`,
		id, tid, checkpointSeq)
	if err != nil {
		return fmt.Errorf("update mission progress: %w", err)
	}
	return nil
}

// DeleteMission removes one mission, scoped to the context tenant.
func (s *PGMissionStore) DeleteMission(ctx context.Context, id uuid.UUID) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM missions WHERE id = $1 AND tenant_id = $2`,
		id, tid)
	if err != nil {
		return fmt.Errorf("delete mission: %w", err)
	}
	return nil
}

// missionMetadata normalizes an opaque JSON blob for JSONB storage.
func missionMetadata(md json.RawMessage) []byte {
	if md == nil {
		return []byte("{}")
	}
	return md
}

// buildMissionGetWhere scopes a single-mission read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildMissionGetWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = $1 AND tenant_id = $2", []any{id, tenantID}
	}
	return " WHERE id = $1", []any{id}
}

// buildMissionListWhere scopes a mission list read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildMissionListWhere(ctx context.Context, opts store.MissionListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, tenantID)
		argIdx++
	}
	if opts.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, opts.Status)
		argIdx++
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type missionRow struct {
	ID            uuid.UUID  `db:"id"`
	TenantID      uuid.UUID  `db:"tenant_id"`
	Name          string     `db:"name"`
	Goals         []byte     `db:"goals"`
	Milestones    []byte     `db:"milestones"`
	Acceptance    []byte     `db:"acceptance"`
	Status        string     `db:"status"`
	AgentID       *uuid.UUID `db:"agent_id"`
	SessionKey    *string    `db:"session_key"`
	CheckpointSeq int        `db:"checkpoint_seq"`
	Metadata      []byte     `db:"metadata"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

func (r missionRow) toStore() store.Mission {
	var goals, milestones, acceptance []string
	scanStringArray(r.Goals, &goals)
	scanStringArray(r.Milestones, &milestones)
	scanStringArray(r.Acceptance, &acceptance)
	return store.Mission{
		ID:            r.ID,
		TenantID:      r.TenantID,
		Name:          r.Name,
		Goals:         goals,
		Milestones:    milestones,
		Acceptance:    acceptance,
		Status:        r.Status,
		AgentID:       r.AgentID,
		SessionKey:    derefStr(r.SessionKey),
		CheckpointSeq: r.CheckpointSeq,
		Metadata:      r.Metadata,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}
