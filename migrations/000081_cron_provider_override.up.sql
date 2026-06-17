-- 000081: per-cron-job LLM provider/model override.
-- Mirrors agent_heartbeats.provider_id/model (see 000022) so scheduled jobs can run
-- on a cheaper provider than the agent default. NULL → falls back to agent default.
ALTER TABLE cron_jobs
    ADD COLUMN IF NOT EXISTS provider_id UUID REFERENCES llm_providers(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS model       VARCHAR(200);
