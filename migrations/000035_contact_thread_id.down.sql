DROP INDEX IF EXISTS idx_channel_contacts_tenant_type_sender;
ALTER TABLE channel_contacts DROP COLUMN IF EXISTS thread_type;
ALTER TABLE channel_contacts DROP COLUMN IF EXISTS thread_id;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender
  ON channel_contacts (tenant_id, channel_type, sender_id);
