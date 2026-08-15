DROP INDEX IF EXISTS idx_vault_docs_basename;
ALTER TABLE vault_documents DROP COLUMN IF EXISTS path_basename;
DROP INDEX IF EXISTS uq_cron_jobs_agent_tenant_name;
