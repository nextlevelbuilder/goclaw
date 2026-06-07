DELETE FROM usage_cap_policies
WHERE source = 'agent_budget_monthly_cents';

DROP INDEX IF EXISTS idx_usage_cap_policies_agent_budget_source;

ALTER TABLE usage_cap_policies
    DROP COLUMN IF EXISTS source;
