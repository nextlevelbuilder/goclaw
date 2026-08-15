ALTER TABLE sessions DROP COLUMN IF EXISTS metadata;
ALTER TABLE user_agent_profiles DROP COLUMN IF EXISTS metadata;
ALTER TABLE pairing_requests DROP COLUMN IF EXISTS metadata;
ALTER TABLE paired_devices DROP COLUMN IF EXISTS metadata;
