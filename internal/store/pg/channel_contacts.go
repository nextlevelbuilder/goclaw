package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)


// PGContactStore implements store.ContactStore backed by Postgres.
type PGContactStore struct {
	db           *sql.DB
	resolveCache *contactResolveCache // tenant-user resolution cache (60s TTL)
}

// NewPGContactStore creates a new PGContactStore.
func NewPGContactStore(db *sql.DB) *PGContactStore {
	return &PGContactStore{db: db, resolveCache: newContactResolveCache()}
}

func (s *PGContactStore) UpsertContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_contacts (channel_type, channel_instance, sender_id, user_id, display_name, username, peer_kind, contact_type, thread_id, thread_type, tenant_id)
		VALUES ($1, NULLIF($2,''), $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''), $8, NULLIF($9,''), NULLIF($10,''), $11)
		ON CONFLICT (tenant_id, channel_type, sender_id, COALESCE(thread_id, '')) DO UPDATE SET
			display_name     = COALESCE(NULLIF($5,''), channel_contacts.display_name),
			username         = COALESCE(NULLIF($6,''), channel_contacts.username),
			user_id          = COALESCE(NULLIF($4,''), channel_contacts.user_id),
			channel_instance = COALESCE(NULLIF($2,''), channel_contacts.channel_instance),
			peer_kind        = COALESCE(NULLIF($7,''), channel_contacts.peer_kind),
			contact_type     = $8,
			thread_type      = COALESCE(NULLIF($10,''), channel_contacts.thread_type),
			last_seen_at     = NOW()`,
		channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType, tenantID,
	)
	return err
}

func contactWhereClause(ctx context.Context, opts store.ContactListOpts) (string, []any, int) {
	var conditions []string
	var args []any
	argIdx := 1

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
			args = append(args, tenantID)
			argIdx++
		}
	}

	if opts.ChannelType != "" {
		conditions = append(conditions, fmt.Sprintf("channel_type = $%d", argIdx))
		args = append(args, opts.ChannelType)
		argIdx++
	}
	if opts.PeerKind != "" {
		conditions = append(conditions, fmt.Sprintf("peer_kind = $%d", argIdx))
		args = append(args, opts.PeerKind)
		argIdx++
	}
	if opts.ContactType != "" {
		conditions = append(conditions, fmt.Sprintf("contact_type = $%d", argIdx))
		args = append(args, opts.ContactType)
		argIdx++
	}
	if opts.Search != "" {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(opts.Search)
		pattern := escaped + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(display_name ILIKE $%d ESCAPE '\\' OR username ILIKE $%d ESCAPE '\\' OR sender_id ILIKE $%d ESCAPE '\\')",
			argIdx, argIdx, argIdx,
		))
		args = append(args, pattern)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	return where, args, argIdx
}

func (s *PGContactStore) ListContacts(ctx context.Context, opts store.ContactListOpts) ([]store.ChannelContact, error) {
	where, args, argIdx := contactWhereClause(ctx, opts)

	query := `SELECT id, channel_type, channel_instance, sender_id, user_id,
		display_name, username, avatar_url, peer_kind, contact_type, thread_id, thread_type, merged_id,
		first_seen_at, last_seen_at
		FROM channel_contacts` + where + " ORDER BY last_seen_at DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)
	argIdx++

	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []store.ChannelContact
	for rows.Next() {
		var c store.ChannelContact
		if err := rows.Scan(
			&c.ID, &c.ChannelType, &c.ChannelInstance, &c.SenderID, &c.UserID,
			&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.ContactType, &c.ThreadID, &c.ThreadType, &c.MergedID,
			&c.FirstSeenAt, &c.LastSeenAt,
		); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *PGContactStore) CountContacts(ctx context.Context, opts store.ContactListOpts) (int, error) {
	where, args, _ := contactWhereClause(ctx, opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_contacts"+where, args...).Scan(&count)
	return count, err
}

func (s *PGContactStore) GetContactsBySenderIDs(ctx context.Context, senderIDs []string) (map[string]store.ChannelContact, error) {
	if len(senderIDs) == 0 {
		return map[string]store.ChannelContact{}, nil
	}

	placeholders := make([]string, len(senderIDs))
	args := make([]any, len(senderIDs))
	for i, id := range senderIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	// Tenant scope: without this filter a low-priv user in tenant A could probe
	// arbitrary sender_ids and exfiltrate displayName/username from tenant B.
	// Sibling reads (GetContactByID, ListMergedContacts) already scope this way.
	tid := store.TenantIDFromContext(ctx)
	args = append(args, tid)
	tenantPH := fmt.Sprintf("$%d", len(args))

	query := fmt.Sprintf(`SELECT DISTINCT ON (sender_id)
		id, channel_type, channel_instance, sender_id, user_id,
		display_name, username, avatar_url, peer_kind, contact_type, thread_id, thread_type, merged_id,
		first_seen_at, last_seen_at
		FROM channel_contacts
		WHERE sender_id IN (%s) AND tenant_id = %s
		ORDER BY sender_id, last_seen_at DESC`, strings.Join(placeholders, ","), tenantPH)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]store.ChannelContact, len(senderIDs))
	for rows.Next() {
		var c store.ChannelContact
		if err := rows.Scan(
			&c.ID, &c.ChannelType, &c.ChannelInstance, &c.SenderID, &c.UserID,
			&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.ContactType, &c.ThreadID, &c.ThreadType, &c.MergedID,
			&c.FirstSeenAt, &c.LastSeenAt,
		); err != nil {
			return nil, err
		}
		result[c.SenderID] = c
	}
	return result, rows.Err()
}

func (s *PGContactStore) GetContactByID(ctx context.Context, id uuid.UUID) (*store.ChannelContact, error) {
	tid := store.TenantIDFromContext(ctx)
	row := s.db.QueryRowContext(ctx,
		`SELECT id, channel_type, channel_instance, sender_id, user_id,
			display_name, username, avatar_url, peer_kind, contact_type,
			thread_id, thread_type, merged_id,
			first_seen_at, last_seen_at
		FROM channel_contacts WHERE id = $1 AND tenant_id = $2`, id, tid)
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

func (s *PGContactStore) GetSenderIDsByContactIDs(ctx context.Context, contactIDs []uuid.UUID) ([]string, error) {
	if len(contactIDs) == 0 {
		return nil, nil
	}
	tid := store.TenantIDFromContext(ctx)
	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs)+1)
	for i, id := range contactIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	args[len(contactIDs)] = tid
	q := fmt.Sprintf("SELECT sender_id FROM channel_contacts WHERE id IN (%s) AND tenant_id = $%d",
		strings.Join(placeholders, ","), len(contactIDs)+1)
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

func (s *PGContactStore) MergeContacts(ctx context.Context, contactIDs []uuid.UUID, tenantUserID uuid.UUID) error {
	if len(contactIDs) == 0 {
		return nil
	}
	tid := store.TenantIDFromContext(ctx)

	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	// $N+1 = tenantUserID, $N+2 = tenant_id
	args = append(args, tenantUserID, tid)
	q := fmt.Sprintf(
		"UPDATE channel_contacts SET merged_id = $%d WHERE id IN (%s) AND tenant_id = $%d",
		len(args)-1, inClause, len(args),
	)
	_, err := s.db.ExecContext(ctx, q, args...)
	if err == nil {
		s.InvalidateContactResolveCache()
	}
	return err
}

func (s *PGContactStore) UnmergeContacts(ctx context.Context, contactIDs []uuid.UUID) error {
	if len(contactIDs) == 0 {
		return nil
	}
	tid := store.TenantIDFromContext(ctx)

	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	args = append(args, tid)
	q := fmt.Sprintf(
		"UPDATE channel_contacts SET merged_id = NULL WHERE id IN (%s) AND tenant_id = $%d",
		inClause, len(args),
	)
	_, err := s.db.ExecContext(ctx, q, args...)
	if err == nil {
		s.InvalidateContactResolveCache()
	}
	return err
}

func (s *PGContactStore) GetContactsByMergedID(ctx context.Context, mergedID uuid.UUID) ([]store.ChannelContact, error) {
	tid := store.TenantIDFromContext(ctx)

	q := `SELECT id, channel_type, channel_instance, sender_id, user_id,
		display_name, username, avatar_url, peer_kind, contact_type, thread_id, thread_type, merged_id,
		first_seen_at, last_seen_at
		FROM channel_contacts WHERE merged_id = $1 AND tenant_id = $2
		ORDER BY last_seen_at DESC`

	rows, err := s.db.QueryContext(ctx, q, mergedID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []store.ChannelContact
	for rows.Next() {
		var c store.ChannelContact
		if err := rows.Scan(
			&c.ID, &c.ChannelType, &c.ChannelInstance, &c.SenderID, &c.UserID,
			&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.ContactType, &c.ThreadID, &c.ThreadType, &c.MergedID,
			&c.FirstSeenAt, &c.LastSeenAt,
		); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}
