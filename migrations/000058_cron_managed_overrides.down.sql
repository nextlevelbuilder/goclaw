DROP INDEX IF EXISTS idx_cron_jobs_managed_key;
DROP INDEX IF EXISTS idx_cron_jobs_managed_by;

ALTER TABLE cron_jobs DROP COLUMN IF EXISTS model;
ALTER TABLE cron_jobs DROP COLUMN IF EXISTS provider;
ALTER TABLE cron_jobs DROP COLUMN IF EXISTS managed;
