package pg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGGroupStore implements store.GroupStore backed by Postgres.
type PGGroupStore struct {
	db *sql.DB
}

func NewPGGroupStore(db *sql.DB) *PGGroupStore {
	return &PGGroupStore{db: db}
}

func (s *PGGroupStore) UpsertGroup(ctx context.Context, channelType, channelInstance, groupID, groupName string, memberCount int) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_groups (tenant_id, channel_type, channel_instance, group_id, group_name, member_count)
		VALUES ($1, $2, NULLIF($3,''), $4, NULLIF($5,''), $6)
		ON CONFLICT (tenant_id, channel_type, group_id) DO UPDATE SET
			group_name       = COALESCE(NULLIF($5,''), channel_groups.group_name),
			channel_instance = COALESCE(NULLIF($3,''), channel_groups.channel_instance),
			member_count     = CASE WHEN $6 > 0 THEN $6 ELSE channel_groups.member_count END,
			last_seen_at     = NOW()`,
		tenantID, channelType, channelInstance, groupID, groupName, memberCount,
	)
	return err
}

func (s *PGGroupStore) ListGroups(ctx context.Context, channelType string) ([]store.ChannelGroup, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	query := `SELECT id, channel_type, channel_instance, group_id, group_name, avatar_url, member_count, first_seen_at, last_seen_at
		FROM channel_groups WHERE tenant_id = $1`
	args := []any{tenantID}
	if channelType != "" {
		query += ` AND channel_type = $2`
		args = append(args, channelType)
	}
	query += ` ORDER BY COALESCE(group_name, group_id)`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

func (s *PGGroupStore) GetGroupsByIDs(ctx context.Context, channelType string, groupIDs []string) (map[string]store.ChannelGroup, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	var query string
	var args []any
	if channelType != "" {
		query, args = buildINQuery(
			`SELECT id, channel_type, channel_instance, group_id, group_name, avatar_url, member_count, first_seen_at, last_seen_at
			 FROM channel_groups WHERE tenant_id = $1 AND channel_type = $2 AND group_id IN `,
			3, groupIDs, tenantID, channelType,
		)
	} else {
		query, args = buildINQuery(
			`SELECT id, channel_type, channel_instance, group_id, group_name, avatar_url, member_count, first_seen_at, last_seen_at
			 FROM channel_groups WHERE tenant_id = $1 AND group_id IN `,
			2, groupIDs, tenantID,
		)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups, err := scanGroups(rows)
	if err != nil {
		return nil, err
	}
	m := make(map[string]store.ChannelGroup, len(groups))
	for _, g := range groups {
		m[g.GroupID] = g
	}
	return m, nil
}

func scanGroups(rows *sql.Rows) ([]store.ChannelGroup, error) {
	var out []store.ChannelGroup
	for rows.Next() {
		var g store.ChannelGroup
		if err := rows.Scan(
			&g.ID, &g.ChannelType, &g.ChannelInstance,
			&g.GroupID, &g.GroupName, &g.AvatarURL,
			&g.MemberCount, &g.FirstSeenAt, &g.LastSeenAt,
		); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// buildINQuery builds a query with IN ($3, $4, ...) placeholders.
func buildINQuery(prefix string, startIdx int, ids []string, prefixArgs ...any) (string, []any) {
	args := make([]any, 0, len(prefixArgs)+len(ids))
	args = append(args, prefixArgs...)
	placeholders := ""
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += fmt.Sprintf("$%d", startIdx+i)
		args = append(args, id)
	}
	return prefix + "(" + placeholders + ")", args
}
