-- Fix channel_contacts.merged_id FK.
-- merged_id stores tenant_users.id, not channel_contacts.id.
BEGIN;

ALTER TABLE channel_contacts
DROP CONSTRAINT IF EXISTS fk_channel_contacts_merged_id;

ALTER TABLE channel_contacts
ADD CONSTRAINT fk_channel_contacts_merged_id
FOREIGN KEY (merged_id)
REFERENCES tenant_users(id)
ON DELETE SET NULL;

COMMIT;
