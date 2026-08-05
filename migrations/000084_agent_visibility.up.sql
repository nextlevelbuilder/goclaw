-- Agent visibility: 'private' (the owner and anyone it is explicitly shared with)
-- or 'org' (every member of the tenant).
--
-- WHY THIS EXISTS
--
-- Personal agents became private to their owner — invisible to org owners and
-- admins alike. That is what we want while someone is a member, and it creates one
-- problem when they are not: a departed member's agents are visible to NOBODY while
-- still firing on whatever schedule they were armed with. A row nobody can see and
-- nobody can stop is worse than either extreme.
--
-- Removing a member now flips their agents to 'org', so the workspace inherits what
-- it was already paying for and can review, re-own or delete it.
--
-- The column is also the primitive for the deliberate version of the same act —
-- "share this agent with the org" — which until now had no representation at all.
--
-- DEFAULT 'private' is the safe direction: every existing row keeps exactly the
-- visibility it has today, and tenant-wide agents stay visible through the
-- separate owner_id='system' rule rather than being migrated onto this one.

ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS visibility VARCHAR(16) NOT NULL DEFAULT 'private';

-- Partial: the only query that reads this asks "which agents in my tenant are
-- org-visible", and org-visible rows are the minority.
CREATE INDEX IF NOT EXISTS idx_agents_tenant_visibility
  ON agents (tenant_id)
  WHERE visibility = 'org' AND deleted_at IS NULL;
