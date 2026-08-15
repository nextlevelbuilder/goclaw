-- Rollback migration 087
ALTER TABLE usage_snapshots DROP CONSTRAINT IF EXISTS fk_usage_snapshots_agent_id;
