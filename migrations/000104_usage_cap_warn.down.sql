DROP INDEX IF EXISTS idx_usage_cap_policies_warn;

ALTER TABLE usage_cap_policies
    DROP COLUMN IF EXISTS warn_at_percent;
