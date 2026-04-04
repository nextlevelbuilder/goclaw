-- Per-agent grants for secure CLI binaries with optional setting overrides.
-- Separates "which agents can use a binary" from "binary credential definition".

-- 1. Create agent grants table
CREATE TABLE secure_cli_agent_grants (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    binary_id       UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    deny_args       JSONB,              -- NULL = use binary default
    deny_verbose    JSONB,              -- NULL = use binary default
    timeout_seconds INTEGER,            -- NULL = use binary default
    tips            TEXT,               -- NULL = use binary default
    enabled         BOOLEAN NOT NULL DEFAULT true,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(binary_id, agent_id, tenant_id)
);

CREATE INDEX idx_scag_binary ON secure_cli_agent_grants(binary_id);
CREATE INDEX idx_scag_agent ON secure_cli_agent_grants(agent_id);
CREATE INDEX idx_scag_tenant ON secure_cli_agent_grants(tenant_id);

-- 2. Add is_global column (default true for backward compat)
ALTER TABLE secure_cli_binaries ADD COLUMN is_global BOOLEAN NOT NULL DEFAULT true;

-- 3. Migrate agent-specific rows to grants table.
--    Copy settings from agent-specific binaries into grants.
INSERT INTO secure_cli_agent_grants (binary_id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, tenant_id)
SELECT id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, tenant_id
FROM secure_cli_binaries
WHERE agent_id IS NOT NULL;

-- 4. For agent-specific rows that HAVE a matching global row (same binary_name + tenant):
--    Re-point grants to the global row, then delete the now-orphaned agent-specific row.
UPDATE secure_cli_agent_grants g
SET binary_id = sub.global_id
FROM (
    SELECT g2.id AS grant_id, b_global.id AS global_id
    FROM secure_cli_agent_grants g2
    JOIN secure_cli_binaries b_agent ON b_agent.id = g2.binary_id AND b_agent.agent_id IS NOT NULL
    JOIN secure_cli_binaries b_global ON b_global.binary_name = b_agent.binary_name
        AND b_global.tenant_id = b_agent.tenant_id
        AND b_global.agent_id IS NULL
) sub
WHERE g.id = sub.grant_id;

DELETE FROM secure_cli_binaries
WHERE agent_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM secure_cli_binaries b2
    WHERE b2.binary_name = secure_cli_binaries.binary_name
      AND b2.tenant_id = secure_cli_binaries.tenant_id
      AND b2.agent_id IS NULL
  );

-- 5. For agent-specific rows WITHOUT a global counterpart:
--    Dedup: keep the row with the smallest id per (binary_name, tenant_id) as the
--    canonical binary definition. Re-point grants from duplicates to the keeper, then delete dupes.

-- 5a. Re-point grants from duplicate rows to the canonical (MIN id) row.
UPDATE secure_cli_agent_grants g
SET binary_id = keeper.keeper_id
FROM (
    SELECT b.id AS dup_id, first_value(b.id) OVER (
        PARTITION BY b.binary_name, b.tenant_id ORDER BY b.id
    ) AS keeper_id
    FROM secure_cli_binaries b
    WHERE b.agent_id IS NOT NULL
) keeper
WHERE g.binary_id = keeper.dup_id
  AND keeper.dup_id != keeper.keeper_id;

-- 5b. Delete duplicate rows (keep only the canonical per binary_name+tenant_id).
DELETE FROM secure_cli_binaries
WHERE agent_id IS NOT NULL
  AND id NOT IN (
    SELECT DISTINCT ON (binary_name, tenant_id) id
    FROM secure_cli_binaries
    WHERE agent_id IS NOT NULL
    ORDER BY binary_name, tenant_id, id
  );

-- 5c. Mark remaining agent-specific rows as restricted (is_global = false).
UPDATE secure_cli_binaries SET is_global = false
WHERE agent_id IS NOT NULL;

-- 6. Drop agent_id column and old indexes.
DROP INDEX IF EXISTS idx_secure_cli_unique_binary_agent;
DROP INDEX IF EXISTS idx_secure_cli_agent_id;
ALTER TABLE secure_cli_binaries DROP COLUMN agent_id;

-- 7. New unique constraint: one binary per name per tenant.
CREATE UNIQUE INDEX idx_secure_cli_unique_binary_tenant
    ON secure_cli_binaries(binary_name, tenant_id);
