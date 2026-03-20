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
	db *sql.DB
}

// NewPGContactStore creates a new PGContactStore.
func NewPGContactStore(db *sql.DB) *PGContactStore {
	return &PGContactStore{db: db}
}

func (s *PGContactStore) UpsertContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_contacts (channel_type, channel_instance, sender_id, user_id, display_name, username, peer_kind)
		VALUES ($1, NULLIF($2,''), $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''))
		ON CONFLICT (channel_type, sender_id) DO UPDATE SET
			display_name     = COALESCE(NULLIF($5,''), channel_contacts.display_name),
			username         = COALESCE(NULLIF($6,''), channel_contacts.username),
			user_id          = COALESCE(NULLIF($4,''), channel_contacts.user_id),
			channel_instance = COALESCE(NULLIF($2,''), channel_contacts.channel_instance),
			peer_kind        = COALESCE(NULLIF($7,''), channel_contacts.peer_kind),
			last_seen_at     = NOW()`,
		channelType, channelInstance, senderID, userID, displayName, username, peerKind,
	)
	return err
}

func contactWhereClause(opts store.ContactListOpts) (string, []any, int) {
	var conditions []string
	var args []any
	argIdx := 1

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
	where, args, argIdx := contactWhereClause(opts)

	query := `SELECT id, channel_type, channel_instance, sender_id, user_id,
		display_name, username, avatar_url, peer_kind, merged_id,
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
			&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.MergedID,
			&c.FirstSeenAt, &c.LastSeenAt,
		); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *PGContactStore) CountContacts(ctx context.Context, opts store.ContactListOpts) (int, error) {
	where, args, _ := contactWhereClause(opts)
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

	query := fmt.Sprintf(`SELECT DISTINCT ON (sender_id)
		id, channel_type, channel_instance, sender_id, user_id,
		display_name, username, avatar_url, peer_kind, merged_id,
		first_seen_at, last_seen_at
		FROM channel_contacts
		WHERE sender_id IN (%s)
		ORDER BY sender_id, last_seen_at DESC`, strings.Join(placeholders, ","))

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
			&c.DisplayName, &c.Username, &c.AvatarURL, &c.PeerKind, &c.MergedID,
			&c.FirstSeenAt, &c.LastSeenAt,
		); err != nil {
			return nil, err
		}
		result[c.SenderID] = c
	}
	return result, rows.Err()
}

func (s *PGContactStore) MergeContacts(ctx context.Context, contactIDs []uuid.UUID) error {
	if len(contactIDs) < 2 {
		return nil
	}

	// Check if any of the contacts already has a merged_id; reuse it.
	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	var existingMergedID *uuid.UUID
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT merged_id FROM channel_contacts WHERE id IN (%s) AND merged_id IS NOT NULL LIMIT 1", inClause),
		args...,
	).Scan(&existingMergedID)

	mergedID := uuid.New()
	if err == nil && existingMergedID != nil {
		mergedID = *existingMergedID
	}

	// Update all contacts with the merged_id.
	args = append(args, mergedID)
	_, err = s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE channel_contacts SET merged_id = $%d WHERE id IN (%s)", len(args), inClause),
		args...,
	)
	return err
}
