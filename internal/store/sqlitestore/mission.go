//go:build sqlite || sqliteonly

package sqlitestore

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

// SQLiteMissionStore implements store.MissionStore backed by SQLite. Goals,
// Milestones, and Acceptance are stored as JSON text; Metadata as JSON text.
type SQLiteMissionStore struct {
	db *sql.DB
}

// NewSQLiteMissionStore creates a SQLite-backed mission store.
func NewSQLiteMissionStore(db *sql.DB) *SQLiteMissionStore {
	return &SQLiteMissionStore{db: db}
}

// CreateMission inserts a new mission. Status defaults to active; the tenant
// comes from the context tenant (master fallback).
func (s *SQLiteMissionStore) CreateMission(ctx context.Context, m *store.Mission) error {
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

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO missions (id, tenant_id, name, goals, milestones, acceptance, status, agent_id, session_key, checkpoint_seq, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID.String(), m.TenantID.String(), m.Name,
		jsonStringArray(m.Goals), jsonStringArray(m.Milestones), jsonStringArray(m.Acceptance),
		m.Status, nilUUIDStr(m.AgentID), m.SessionKey, m.CheckpointSeq,
		missionMetadataString(m.Metadata), m.CreatedAt.Format(time.RFC3339Nano), m.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("create mission: insert: %w", err)
	}
	return nil
}

// GetMission returns one mission by id, scoped to the context tenant.
func (s *SQLiteMissionStore) GetMission(ctx context.Context, id uuid.UUID) (*store.Mission, error) {
	where, args := buildSQLiteMissionGetWhere(ctx, id)
	q := `SELECT id, tenant_id, name, goals, milestones, acceptance, status, agent_id, session_key, checkpoint_seq, metadata, created_at, updated_at
		 FROM missions` + where
	var row missionRow
	if err := scanMissionRow(s.db.QueryRowContext(ctx, q, args...).Scan, &row); err != nil {
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
func (s *SQLiteMissionStore) ListMissions(ctx context.Context, opts store.MissionListOpts) ([]store.Mission, error) {
	where, args := buildSQLiteMissionListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, name, goals, milestones, acceptance, status, agent_id, session_key, checkpoint_seq, metadata, created_at, updated_at
		 FROM missions` + where +
		` ORDER BY created_at DESC, id DESC` +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.Mission
	for rows.Next() {
		var row missionRow
		if err := scanMissionRow(rows.Scan, &row); err != nil {
			return nil, err
		}
		items = append(items, row.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.Mission{}
	}
	return items, nil
}

// UpdateMissionStatus transitions one mission's status, scoped to the context
// tenant. The target status must be a known status.
func (s *SQLiteMissionStore) UpdateMissionStatus(ctx context.Context, id uuid.UUID, status string) error {
	if !store.ValidMissionStatus(status) {
		return fmt.Errorf("update mission status: unknown status %q", status)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE missions SET status = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		status, now, id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("update mission status: %w", err)
	}
	return nil
}

// UpdateMissionProgress links the mission to the latest durable checkpoint
// snapshot seq, scoped to the context tenant.
func (s *SQLiteMissionStore) UpdateMissionProgress(ctx context.Context, id uuid.UUID, checkpointSeq int) error {
	if checkpointSeq < 0 {
		return fmt.Errorf("update mission progress: negative checkpoint seq %d", checkpointSeq)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE missions SET checkpoint_seq = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		checkpointSeq, now, id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("update mission progress: %w", err)
	}
	return nil
}

// DeleteMission removes one mission, scoped to the context tenant.
func (s *SQLiteMissionStore) DeleteMission(ctx context.Context, id uuid.UUID) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM missions WHERE id = ? AND tenant_id = ?`,
		id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("delete mission: %w", err)
	}
	return nil
}

// missionMetadataString normalizes an opaque JSON blob for TEXT storage.
func missionMetadataString(md json.RawMessage) string {
	if md == nil {
		return "{}"
	}
	return string(md)
}

// buildSQLiteMissionGetWhere scopes a single-mission read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteMissionGetWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = ? AND tenant_id = ?", []any{id.String(), tenantID.String()}
	}
	return " WHERE id = ?", []any{id.String()}
}

// buildSQLiteMissionListWhere scopes a mission list read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteMissionListWhere(ctx context.Context, opts store.MissionListOpts) (string, []any) {
	var conditions []string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID.String())
	}
	if opts.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, opts.Status)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type missionRow struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	Goals         sql.NullString
	Milestones    sql.NullString
	Acceptance    sql.NullString
	Status        string
	AgentID       sql.NullString
	SessionKey    sql.NullString
	CheckpointSeq int
	Metadata      sql.NullString
	CreatedAt     sqliteTime
	UpdatedAt     sqliteTime
}

// scanMissionRow scans one missions row, converting SQLite's TEXT UUIDs,
// arrays, timestamps, and nullable columns into Go values.
func scanMissionRow(scan func(dest ...any) error, r *missionRow) error {
	var id, tenantID string
	var createdAt, updatedAt sqliteTime
	if err := scan(
		&id, &tenantID, &r.Name, &r.Goals, &r.Milestones, &r.Acceptance, &r.Status,
		&r.AgentID, &r.SessionKey, &r.CheckpointSeq, &r.Metadata, &createdAt, &updatedAt,
	); err != nil {
		return err
	}
	r.ID = uuid.MustParse(id)
	r.TenantID = uuid.MustParse(tenantID)
	r.CreatedAt = createdAt
	r.UpdatedAt = updatedAt
	return nil
}

func (r missionRow) toStore() store.Mission {
	var goals, milestones, acceptance []string
	scanJSONStringArray([]byte(r.Goals.String), &goals)
	scanJSONStringArray([]byte(r.Milestones.String), &milestones)
	scanJSONStringArray([]byte(r.Acceptance.String), &acceptance)
	var agentID *uuid.UUID
	if r.AgentID.Valid && r.AgentID.String != "" {
		if parsed, err := uuid.Parse(r.AgentID.String); err == nil {
			agentID = &parsed
		}
	}
	return store.Mission{
		ID:            r.ID,
		TenantID:      r.TenantID,
		Name:          r.Name,
		Goals:         goals,
		Milestones:    milestones,
		Acceptance:    acceptance,
		Status:        r.Status,
		AgentID:       agentID,
		SessionKey:    r.SessionKey.String,
		CheckpointSeq: r.CheckpointSeq,
		Metadata:      json.RawMessage(r.Metadata.String),
		CreatedAt:     r.CreatedAt.Time,
		UpdatedAt:     r.UpdatedAt.Time,
	}
}
