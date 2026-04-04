//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteContactStore implements store.ContactStore backed by SQLite.
type SQLiteContactStore struct {
	db *sql.DB
}

func NewSQLiteContactStore(db *sql.DB) *SQLiteContactStore {
	return &SQLiteContactStore{db: db}
}

func (s *SQLiteContactStore) UpsertContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_contacts (channel_type, channel_instance, sender_id, user_id, display_name, username, peer_kind, contact_type, thread_id, thread_type, tenant_id)
		VALUES (?, NULLIF(?,?), ?, NULLIF(?,?), NULLIF(?,?), NULLIF(?,?), NULLIF(?,?), ?, NULLIF(?,?), NULLIF(?,?), ?)
		ON CONFLICT (tenant_id, channel_type, sender_id, COALESCE(thread_id, '')) DO UPDATE SET
			display_name     = COALESCE(NULLIF(excluded.display_name,''), channel_contacts.display_name),
			username         = COALESCE(NULLIF(excluded.username,''), channel_contacts.username),
			user_id          = COALESCE(NULLIF(excluded.user_id,''), channel_contacts.user_id),
			channel_instance = COALESCE(NULLIF(excluded.channel_instance,''), channel_contacts.channel_instance),
			peer_kind        = COALESCE(NULLIF(excluded.peer_kind,''), channel_contacts.peer_kind),
			contact_type     = excluded.contact_type,
			thread_type      = COALESCE(NULLIF(excluded.thread_type,''), channel_contacts.thread_type),
			last_seen_at     = CURRENT_TIMESTAMP`,
		channelType,
		channelInstance, "",
		senderID,
		userID, "",
		displayName, "",
		username, "",
		peerKind, "",
		contactType,
		threadID, "",
		threadType, "",
		tenantID,
	)
	return err
}

func contactWhereSQLite(ctx context.Context, opts store.ContactListOpts) (string, []any) {
	var conditions []string
	var args []any

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, "tenant_id = ?")
			args = append(args, tenantID)
		}
	}

	if opts.ChannelType != "" {
		conditions = append(conditions, "channel_type = ?")
		args = append(args, opts.ChannelType)
	}
	if opts.PeerKind != "" {
		conditions = append(conditions, "peer_kind = ?")
		args = append(args, opts.PeerKind)
	}
	if opts.ContactType != "" {
		conditions = append(conditions, "contact_type = ?")
		args = append(args, opts.ContactType)
	}
	if opts.Search != "" {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(opts.Search)
		pattern := escaped + "%"
		conditions = append(conditions, "(display_name LIKE ? ESCAPE '\\' OR username LIKE ? ESCAPE '\\' OR sender_id LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern, pattern)
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	return where, args
}

const contactSelectCols = `id, channel_type, channel_instance, sender_id, user_id,
		display_name, username, avatar_url, peer_kind, contact_type, thread_id, thread_type, merged_id,
		first_seen_at, last_seen_at`

func scanContact(rows *sql.Rows) (store.ChannelContact, error) {
	var c store.ChannelContact
	err := rows.Scan(
		&c.ID, &c.ChannelType, &c.ChannelInstance, &c.SenderID, &c.UserID,
		&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.ContactType, &c.ThreadID, &c.ThreadType, &c.MergedID,
		&c.FirstSeenAt, &c.LastSeenAt,
	)
	return c, err
}

func (s *SQLiteContactStore) ListContacts(ctx context.Context, opts store.ContactListOpts) ([]store.ChannelContact, error) {
	where, args := contactWhereSQLite(ctx, opts)

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + contactSelectCols + ` FROM channel_contacts` + where +
		fmt.Sprintf(" ORDER BY last_seen_at DESC LIMIT %d", limit)
	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []store.ChannelContact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *SQLiteContactStore) CountContacts(ctx context.Context, opts store.ContactListOpts) (int, error) {
	where, args := contactWhereSQLite(ctx, opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_contacts"+where, args...).Scan(&count)
	return count, err
}

func (s *SQLiteContactStore) GetContactsBySenderIDs(ctx context.Context, senderIDs []string) (map[string]store.ChannelContact, error) {
	if len(senderIDs) == 0 {
		return map[string]store.ChannelContact{}, nil
	}

	placeholders := make([]string, len(senderIDs))
	args := make([]any, len(senderIDs))
	for i, id := range senderIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// SQLite has no DISTINCT ON; emulate with GROUP BY + MAX rowid trick via subquery
	query := `SELECT ` + contactSelectCols + `
		FROM channel_contacts
		WHERE sender_id IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY sender_id
		ORDER BY sender_id, last_seen_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]store.ChannelContact, len(senderIDs))
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		result[c.SenderID] = c
	}
	return result, rows.Err()
}

func (s *SQLiteContactStore) GetContactByID(ctx context.Context, id uuid.UUID) (*store.ChannelContact, error) {
	tid := store.TenantIDFromContext(ctx)
	row := s.db.QueryRowContext(ctx,
		`SELECT `+contactSelectCols+`
		FROM channel_contacts WHERE id = ? AND tenant_id = ?`, id, tid)
	var c store.ChannelContact
	if err := row.Scan(
		&c.ID, &c.ChannelType, &c.ChannelInstance, &c.SenderID, &c.UserID,
		&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.ContactType,
		&c.ThreadID, &c.ThreadType, &c.MergedID,
		&c.FirstSeenAt, &c.LastSeenAt,
	); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *SQLiteContactStore) GetSenderIDsByContactIDs(ctx context.Context, contactIDs []uuid.UUID) ([]string, error) {
	if len(contactIDs) == 0 {
		return nil, nil
	}
	tid := store.TenantIDFromContext(ctx)
	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs)+1)
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	args[len(contactIDs)] = tid
	q := fmt.Sprintf("SELECT sender_id FROM channel_contacts WHERE id IN (%s) AND tenant_id = ?",
		strings.Join(placeholders, ","))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		result = append(result, sid)
	}
	return result, rows.Err()
}

func (s *SQLiteContactStore) MergeContacts(ctx context.Context, contactIDs []uuid.UUID, tenantUserID uuid.UUID) error {
	if len(contactIDs) == 0 {
		return nil
	}
	tid := store.TenantIDFromContext(ctx)

	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	args = append(args, tenantUserID, tid)
	q := fmt.Sprintf(
		"UPDATE channel_contacts SET merged_id = ? WHERE id IN (%s) AND tenant_id = ?",
		strings.Join(placeholders, ","),
	)
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

func (s *SQLiteContactStore) UnmergeContacts(ctx context.Context, contactIDs []uuid.UUID) error {
	if len(contactIDs) == 0 {
		return nil
	}
	tid := store.TenantIDFromContext(ctx)

	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	args = append(args, tid)
	q := fmt.Sprintf(
		"UPDATE channel_contacts SET merged_id = NULL WHERE id IN (%s) AND tenant_id = ?",
		strings.Join(placeholders, ","),
	)
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

func (s *SQLiteContactStore) GetContactsByMergedID(ctx context.Context, mergedID uuid.UUID) ([]store.ChannelContact, error) {
	tid := store.TenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+contactSelectCols+`
		FROM channel_contacts WHERE merged_id = ? AND tenant_id = ?
		ORDER BY last_seen_at DESC`, mergedID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []store.ChannelContact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *SQLiteContactStore) ResolveTenantUserID(ctx context.Context, channelType, senderID string) (string, error) {
	tid := store.TenantIDFromContext(ctx)
	var tenantUserID string
	err := s.db.QueryRowContext(ctx,
		`SELECT tu.user_id FROM channel_contacts cc
		 JOIN tenant_users tu ON cc.merged_id = tu.id
		 WHERE cc.tenant_id = ? AND cc.channel_type = ? AND cc.sender_id = ?
		 AND cc.merged_id IS NOT NULL`,
		tid, channelType, senderID,
	).Scan(&tenantUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tenantUserID, err
}
