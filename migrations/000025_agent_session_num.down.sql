DROP TRIGGER IF EXISTS trg_sessions_agent_num ON sessions;
DROP FUNCTION IF EXISTS set_agent_session_num();
ALTER TABLE sessions DROP COLUMN IF EXISTS agent_session_num;
