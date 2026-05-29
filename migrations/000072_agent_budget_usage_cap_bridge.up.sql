ALTER TABLE usage_cap_policies
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_cap_policies_agent_budget_source
    ON usage_cap_policies (tenant_id, agent_id)
    WHERE source = 'agent_budget_monthly_cents';

INSERT INTO usage_cap_policies (
    tenant_id, agent_id, window_key, max_cost_micros, enabled, priority, source
)
SELECT
    tenant_id,
    id,
    'month',
    budget_monthly_cents::BIGINT * 10000,
    true,
    90,
    'agent_budget_monthly_cents'
FROM agents
WHERE deleted_at IS NULL
  AND budget_monthly_cents IS NOT NULL
  AND budget_monthly_cents > 0
ON CONFLICT DO NOTHING;
