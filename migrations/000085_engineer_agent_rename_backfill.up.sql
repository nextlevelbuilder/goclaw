-- 000085_engineer_agent_rename_backfill.up.sql
--
-- Backfills the display name of every user's personal "coder" starter agent
-- from "Coder" to "Engineer". agent_templates.go's starterAgentTemplates is
-- copied by value into a new DB row per user at seed time
-- (maybeSeedStarterTemplates, internal/http/agents.go) — it is a one-time
-- clone, not a live reference — so renaming the Go struct only affects users
-- seeded after this deploy. Existing rows are frozen at "Coder" until backfilled.
--
-- agent_key gets a random suffix at seed time (coder-<uuid>), so match on the
-- prefix rather than an exact key. Guarded on display_name='Coder' so this is
-- a no-op for anyone who has already renamed their own copy.

UPDATE agents
SET display_name = 'Engineer'
WHERE agent_key LIKE 'coder-%'
  AND agent_type = 'predefined'
  AND display_name = 'Coder'
  AND deleted_at IS NULL;
