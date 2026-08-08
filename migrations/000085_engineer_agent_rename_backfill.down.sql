-- 000085_engineer_agent_rename_backfill.down.sql
--
-- Reverts the "Coder" -> "Engineer" backfill. Only touches rows whose
-- display_name is still exactly "Engineer" — a user who has since renamed
-- their own copy to something else stays untouched.

UPDATE agents
SET display_name = 'Coder'
WHERE agent_key LIKE 'coder-%'
  AND agent_type = 'predefined'
  AND display_name = 'Engineer'
  AND deleted_at IS NULL;
