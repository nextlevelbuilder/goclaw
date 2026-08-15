-- Roll back only the corrective FK.
-- Do not recreate the old self-reference FK because it is incompatible with valid data.
BEGIN;

ALTER TABLE channel_contacts
DROP CONSTRAINT IF EXISTS fk_channel_contacts_merged_id;

COMMIT;
