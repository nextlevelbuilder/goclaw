-- Add media_refs column to listen_raw_messages for storing media attachment references.
-- Each raw message can have 0-N media attachments persisted alongside the text body.
ALTER TABLE listen_raw_messages ADD COLUMN media_refs JSONB NOT NULL DEFAULT '[]';
