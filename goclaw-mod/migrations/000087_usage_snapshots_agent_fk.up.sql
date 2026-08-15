-- Migration 087: FK constraint for usage_snapshots.agent_id
--
-- Pre-check audit (should return 0 violations on a clean DB):
--   SELECT COUNT(*) FROM usage_snapshots
--   WHERE agent_id IS NOT NULL
--     AND agent_id NOT IN (SELECT id FROM agents);
--
-- Orphan cleanup (idempotent guard):
DELETE FROM usage_snapshots
WHERE agent_id IS NOT NULL
  AND agent_id NOT IN (SELECT id FROM agents);

-- Add FK constraint: usage_snapshots.agent_id → agents(id) ON DELETE SET NULL
-- agent_id is nullable — rows with NULL remain for aggregate/rollup totals.
ALTER TABLE usage_snapshots
    ADD CONSTRAINT fk_usage_snapshots_agent_id
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;
