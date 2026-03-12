package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGActivityStore implements store.ActivityStore backed by Postgres.
type PGActivityStore struct {
	db *sql.DB
}

// NewPGActivityStore creates a new PGActivityStore.
func NewPGActivityStore(db *sql.DB) *PGActivityStore {
	return &PGActivityStore{db: db}
}

func (s *PGActivityStore) Log(ctx context.Context, entry *store.ActivityLog) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO activity_logs (actor_type, actor_id, action, entity_type, entity_id, details, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ActorType, entry.ActorID, entry.Action,
		entry.EntityType, entry.EntityID, entry.Details, entry.IPAddress,
	)
	return err
}

func (s *PGActivityStore) List(ctx context.Context, opts store.ActivityListOpts) ([]store.ActivityLog, error) {
	where, args := buildActivityWhere(opts)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, opts.Offset)

	query := fmt.Sprintf(
		`SELECT id, actor_type, actor_id, action, COALESCE(entity_type,''), COALESCE(entity_id,''), details, COALESCE(ip_address,''), created_at
		 FROM activity_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, len(args)-1, len(args),
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ActivityLog
	for rows.Next() {
		var a store.ActivityLog
		if err := rows.Scan(&a.ID, &a.ActorType, &a.ActorID, &a.Action, &a.EntityType, &a.EntityID, &a.Details, &a.IPAddress, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *PGActivityStore) Count(ctx context.Context, opts store.ActivityListOpts) (int, error) {
	where, args := buildActivityWhere(opts)
	query := fmt.Sprintf("SELECT COUNT(*) FROM activity_logs %s", where)

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func buildActivityWhere(opts store.ActivityListOpts) (string, []any) {
	var conditions []string
	var args []any
	idx := 1

	if opts.ActorType != "" {
		conditions = append(conditions, fmt.Sprintf("actor_type = $%d", idx))
		args = append(args, opts.ActorType)
		idx++
	}
	if opts.ActorID != "" {
		conditions = append(conditions, fmt.Sprintf("actor_id = $%d", idx))
		args = append(args, opts.ActorID)
		idx++
	}
	if opts.Action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", idx))
		args = append(args, opts.Action)
		idx++
	}
	if opts.EntityType != "" {
		conditions = append(conditions, fmt.Sprintf("entity_type = $%d", idx))
		args = append(args, opts.EntityType)
		idx++
	}
	if opts.EntityID != "" {
		conditions = append(conditions, fmt.Sprintf("entity_id = $%d", idx))
		args = append(args, opts.EntityID)
		idx++
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}
