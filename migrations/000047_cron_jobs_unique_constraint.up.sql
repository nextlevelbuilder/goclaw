-- Deduplicate cron_jobs: keep most recently updated row per (agent_id, tenant_id, name).
DELETE FROM cron_jobs
WHERE id NOT IN (
  SELECT DISTINCT ON (agent_id, tenant_id, name) id
  FROM cron_jobs
  ORDER BY agent_id, tenant_id, name, updated_at DESC
);

-- Create unique index (will succeed after dedup).
CREATE UNIQUE INDEX IF NOT EXISTS uq_cron_jobs_agent_tenant_name
  ON cron_jobs (agent_id, tenant_id, name);

-- Generated column for path basename (case-insensitive wikilink resolution).
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS path_basename TEXT
  GENERATED ALWAYS AS (lower(regexp_replace(path, '.+/', ''))) STORED;

-- Index for fast basename lookup by tenant.
CREATE INDEX IF NOT EXISTS idx_vault_docs_basename
  ON vault_documents(tenant_id, path_basename);
