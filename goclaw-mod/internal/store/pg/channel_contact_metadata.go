package pg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UpsertContactWithMetadata keeps the canonical contact name intact while
// persisting platform-specific presentation metadata.
func (s *PGContactStore) UpsertContactWithMetadata(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string, metadata map[string]string) error {
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
		SET metadata = $1::jsonb
		WHERE tenant_id = $2
		  AND channel_type = $3
		  AND sender_id = $4
		  AND COALESCE(thread_id, '') = $5`,
		string(encoded), tenantID, channelType, senderID, threadID,
	)
	return err
}
