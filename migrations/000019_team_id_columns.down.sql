-- Drop performance indexes for team_tasks
DROP INDEX IF EXISTS idx_tt_owner_status;
DROP INDEX IF EXISTS idx_tt_blocked_by;

-- Revert: remove team_id from memory, KG, traces, spans, cron, and sessions tables.

-- Sessions
DROP INDEX IF EXISTS idx_sessions_team;
ALTER TABLE sessions DROP COLUMN IF EXISTS team_id;

-- Cron run logs
DROP INDEX IF EXISTS idx_cron_run_logs_team;
ALTER TABLE cron_run_logs DROP COLUMN IF EXISTS team_id;

-- Cron jobs
DROP INDEX IF EXISTS idx_cron_jobs_team;
ALTER TABLE cron_jobs DROP COLUMN IF EXISTS team_id;

-- Spans
DROP INDEX IF EXISTS idx_spans_team;
ALTER TABLE spans DROP COLUMN IF EXISTS team_id;

-- Traces
DROP INDEX IF EXISTS idx_traces_team;
ALTER TABLE traces DROP COLUMN IF EXISTS team_id;

-- KG relations
DROP INDEX IF EXISTS idx_kg_relations_team;
ALTER TABLE kg_relations DROP COLUMN IF EXISTS team_id;

-- KG entities
DROP INDEX IF EXISTS idx_kg_entities_team;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS team_id;

-- Memory chunks
DROP INDEX IF EXISTS idx_memchunk_team;
ALTER TABLE memory_chunks DROP COLUMN IF EXISTS team_id;

-- Memory documents
DROP INDEX IF EXISTS idx_memdoc_team;
ALTER TABLE memory_documents DROP COLUMN IF EXISTS team_id;
