-- Add team_id to memory, KG, traces, and spans tables.
-- Enables future team-scoped memory, KG, and tracing.
-- Nullable: existing rows keep NULL (personal scope). Team-scoped rows will set team_id.

-- Memory documents
ALTER TABLE memory_documents ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_memdoc_team ON memory_documents(team_id) WHERE team_id IS NOT NULL;

-- Memory chunks
ALTER TABLE memory_chunks ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_memchunk_team ON memory_chunks(team_id) WHERE team_id IS NOT NULL;

-- KG entities
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_kg_entities_team ON kg_entities(team_id) WHERE team_id IS NOT NULL;

-- KG relations
ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_kg_relations_team ON kg_relations(team_id) WHERE team_id IS NOT NULL;

-- Traces
ALTER TABLE traces ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_traces_team ON traces(team_id, created_at DESC) WHERE team_id IS NOT NULL;

-- Spans
ALTER TABLE spans ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_spans_team ON spans(team_id) WHERE team_id IS NOT NULL;

-- Cron jobs
ALTER TABLE cron_jobs ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_cron_jobs_team ON cron_jobs(team_id) WHERE team_id IS NOT NULL;

-- Cron run logs
ALTER TABLE cron_run_logs ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_cron_run_logs_team ON cron_run_logs(team_id) WHERE team_id IS NOT NULL;

-- Sessions
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_team ON sessions(team_id) WHERE team_id IS NOT NULL;

-- Performance indexes for team_tasks
-- GIN index for unblockDependentTasks: WHERE $1 = ANY(blocked_by)
CREATE INDEX IF NOT EXISTS idx_tt_blocked_by ON team_tasks USING GIN(blocked_by);

-- Composite index for ListIdleMembers subquery: NOT EXISTS(... owner_agent_id = ... AND status = ...)
CREATE INDEX IF NOT EXISTS idx_tt_owner_status ON team_tasks(team_id, owner_agent_id, status);
