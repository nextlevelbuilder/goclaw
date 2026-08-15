//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UpsertContactWithMetadata keeps the canonical contact name intact while
// persisting platform-specific presentation metadata.
func (s *SQLiteContactStore) UpsertContactWithMetadata(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string, metadata map[string]string) error {
	if err := s.UpsertContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType); err != nil {
		return err
	}
	if len(metadata) == 0 {
		return nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal contact metadata: %w", err)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE channel_contacts
		SET metadata = ?
		WHERE tenant_id = ?
		  AND channel_type = ?
		  AND sender_id = ?
		  AND COALESCE(thread_id, '') = ?`,
		string(encoded), tenantID.String(), channelType, senderID, threadID,
	)
	return err
}
