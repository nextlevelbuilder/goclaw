ALTER TABLE cron_jobs ADD COLUMN IF NOT EXISTS managed JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE cron_jobs ADD COLUMN IF NOT EXISTS provider VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN IF NOT EXISTS model VARCHAR(255) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_cron_jobs_managed_by
  ON cron_jobs ((managed->>'by'))
  WHERE managed <> '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_cron_jobs_managed_key
  ON cron_jobs ((managed->>'key'))
  WHERE managed ? 'key';
