ALTER TABLE channel_contacts ADD COLUMN thread_id VARCHAR(100);
ALTER TABLE channel_contacts ADD COLUMN thread_type VARCHAR(20);

-- Fix sender_id: strip "|username" suffix, keep only numeric ID.
-- Step 1: Delete "|username" rows where a numeric-only row already exists (avoid UNIQUE conflict).
DELETE FROM channel_contacts
WHERE sender_id LIKE '%|%'
  AND EXISTS (
    SELECT 1 FROM channel_contacts c2
    WHERE c2.tenant_id = channel_contacts.tenant_id
      AND c2.channel_type = channel_contacts.channel_type
      AND c2.sender_id = split_part(channel_contacts.sender_id, '|', 1)
      AND COALESCE(c2.thread_id, '') = COALESCE(channel_contacts.thread_id, '')
  );
-- Step 2: Update remaining "|username" rows (no numeric counterpart) to strip suffix.
UPDATE channel_contacts
SET sender_id = split_part(sender_id, '|', 1)
WHERE sender_id LIKE '%|%';

DROP INDEX IF EXISTS idx_channel_contacts_tenant_type_sender;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender
  ON channel_contacts (tenant_id, channel_type, sender_id, COALESCE(thread_id, ''));
