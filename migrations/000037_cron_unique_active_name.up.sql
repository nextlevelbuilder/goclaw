-- Rename duplicate active jobs before adding constraint (safe: no data loss, reversible).
-- Oldest job (smallest created_at) keeps its name; newer duplicates get a '-dup-{id[:8]}' suffix.
WITH dupes AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY tenant_id, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
                     COALESCE(user_id, ''), name
        ORDER BY created_at ASC
    ) AS rn
    FROM cron_jobs
    WHERE enabled = true
)
UPDATE cron_jobs
SET name = name || '-dup-' || substr(id::text, 1, 8)
WHERE id IN (SELECT id FROM dupes WHERE rn > 1);

-- Partial unique index: one active job per name per tenant+agent+user.
CREATE UNIQUE INDEX idx_cron_jobs_unique_active_name
    ON cron_jobs (tenant_id, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'), COALESCE(user_id, ''), name)
    WHERE enabled = true;
