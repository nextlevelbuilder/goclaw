ALTER TABLE sessions ADD COLUMN IF NOT EXISTS agent_session_num INT;

-- Trigger function: assigns per-agent sequential number on INSERT.
-- Uses pg_advisory_xact_lock to prevent race conditions when two sessions
-- for the same agent are created concurrently.
CREATE OR REPLACE FUNCTION set_agent_session_num()
RETURNS TRIGGER AS $$
DECLARE
  agent_key TEXT;
BEGIN
  agent_key := split_part(NEW.session_key, ':', 2);
  PERFORM pg_advisory_xact_lock(hashtext(agent_key));
  SELECT COALESCE(MAX(agent_session_num), 0) + 1
    INTO NEW.agent_session_num
    FROM sessions
   WHERE split_part(session_key, ':', 2) = agent_key;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_sessions_agent_num
  BEFORE INSERT ON sessions
  FOR EACH ROW
  EXECUTE FUNCTION set_agent_session_num();

-- Backfill existing rows using ROW_NUMBER per agent key
UPDATE sessions
   SET agent_session_num = sub.rn
  FROM (
    SELECT id,
           ROW_NUMBER() OVER (
             PARTITION BY split_part(session_key, ':', 2)
             ORDER BY created_at
           ) AS rn
      FROM sessions
  ) sub
 WHERE sessions.id = sub.id;
