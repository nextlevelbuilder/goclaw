-- Budget threshold alerts for usage cap policies.
-- warn_at_percent (0-100, NULL = disabled): when Reconcile drives a policy's
-- used/reserved cost past this percentage of its limit, the service records a
-- usage_cap_events row (decision='warn') and fires a best-effort webhook alert
-- (reason goclaw.budget). Additive only.
ALTER TABLE usage_cap_policies
    ADD COLUMN IF NOT EXISTS warn_at_percent NUMERIC(5,2)
        CHECK (warn_at_percent IS NULL OR (warn_at_percent >= 0 AND warn_at_percent <= 100));

CREATE INDEX IF NOT EXISTS idx_usage_cap_policies_warn
    ON usage_cap_policies (warn_at_percent)
    WHERE warn_at_percent IS NOT NULL;
