//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteActivityStore implements store.ActivityStore backed by SQLite.
type SQLiteActivityStore struct {
	db *sql.DB
}

// NewSQLiteActivityStore creates a new SQLiteActivityStore.
func NewSQLiteActivityStore(db *sql.DB) *SQLiteActivityStore {
	return &SQLiteActivityStore{db: db}
}

func (s *SQLiteActivityStore) Log(ctx context.Context, entry *store.ActivityLog) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	id := uuid.New()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO activity_logs (id, actor_type, actor_id, action, entity_type, entity_id, details, ip_address, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, entry.ActorType, entry.ActorID, entry.Action,
		entry.EntityType, entry.EntityID, entry.Details, entry.IPAddress, tenantID,
	)
	return err
}

func (s *SQLiteActivityStore) List(ctx context.Context, opts store.ActivityListOpts) ([]store.ActivityLog, error) {
	where, args := buildActivityWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, opts.Offset)

	query := fmt.Sprintf(
		`SELECT id, actor_type, actor_id, action, COALESCE(entity_type,''), COALESCE(entity_id,''), COALESCE(details,'null'), COALESCE(ip_address,''), created_at
		 FROM activity_logs %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		where,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ActivityLog
	for rows.Next() {
		var a store.ActivityLog
		var createdAt sqliteTime
		if err := rows.Scan(&a.ID, &a.ActorType, &a.ActorID, &a.Action, &a.EntityType, &a.EntityID, &a.Details, &a.IPAddress, &createdAt); err != nil {
			return nil, err
		}
		a.CreatedAt = createdAt.Time
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *SQLiteActivityStore) Count(ctx context.Context, opts store.ActivityListOpts) (int, error) {
	where, args := buildActivityWhere(ctx, opts)
	query := fmt.Sprintf("SELECT COUNT(*) FROM activity_logs %s", where)

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (s *SQLiteActivityStore) Aggregate(ctx context.Context, opts store.ActivityAggregateOpts) ([]store.ActivityAggregateBucket, int, error) {
	column, ok := activityAggregateColumn(opts.GroupBy)
	if !ok {
		return nil, 0, fmt.Errorf("invalid group_by")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := buildActivityWhere(ctx, opts.ActivityListOpts)
	total, err := s.Count(ctx, opts.ActivityListOpts)
	if err != nil {
		return nil, 0, err
	}
	args = append(args, limit)
	query := fmt.Sprintf(
		`SELECT COALESCE(%s, ''), COUNT(*), MAX(created_at)
		 FROM activity_logs %s
		 GROUP BY %s
		 ORDER BY COUNT(*) DESC, MAX(created_at) DESC
		 LIMIT ?`,
		column, where, column,
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var buckets []store.ActivityAggregateBucket
	for rows.Next() {
		var b store.ActivityAggregateBucket
		var lastSeen sqliteTime
		if err := rows.Scan(&b.Key, &b.Count, &lastSeen); err != nil {
			return nil, 0, err
		}
		b.LastSeen = lastSeen.Time
		buckets = append(buckets, b)
	}
	return buckets, total, rows.Err()
}

func buildActivityWhere(ctx context.Context, opts store.ActivityListOpts) (string, []any) {
	var conditions []string
	var args []any

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, "tenant_id = ?")
			args = append(args, tenantID)
		}
	}

	if opts.ActorType != "" {
		conditions = append(conditions, "actor_type = ?")
		args = append(args, opts.ActorType)
	}
	if opts.ActorID != "" {
		conditions = append(conditions, "actor_id = ?")
		args = append(args, opts.ActorID)
	}
	if opts.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, opts.Action)
	}
	if opts.EntityType != "" {
		conditions = append(conditions, "entity_type = ?")
		args = append(args, opts.EntityType)
	}
	if opts.EntityID != "" {
		conditions = append(conditions, "entity_id = ?")
		args = append(args, opts.EntityID)
	}
	if opts.From != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, *opts.From)
	}
	if opts.To != nil {
		conditions = append(conditions, "created_at < ?")
		args = append(args, *opts.To)
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func activityAggregateColumn(groupBy string) (string, bool) {
	switch groupBy {
	case "action":
		return "action", true
	case "actor_type":
		return "actor_type", true
	case "entity_type":
		return "entity_type", true
	case "actor_id":
		return "actor_id", true
	default:
		return "", false
	}
}
