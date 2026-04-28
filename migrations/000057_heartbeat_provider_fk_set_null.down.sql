-- Reverse: drop the SET NULL FK and restore implicit RESTRICT.
-- Note: pre-existing NULL provider_id rows remain NULL (RESTRICT FK accepts NULL trivially).
DO $$
DECLARE
  cname text;
BEGIN
  SELECT conname INTO cname
    FROM pg_constraint
   WHERE conrelid = 'agent_heartbeats'::regclass
     AND confrelid = 'llm_providers'::regclass
     AND contype = 'f';
  IF cname IS NOT NULL THEN
    EXECUTE format('ALTER TABLE agent_heartbeats DROP CONSTRAINT %I', cname);
  END IF;
END $$;

ALTER TABLE agent_heartbeats
  ADD CONSTRAINT agent_heartbeats_provider_id_fkey
    FOREIGN KEY (provider_id) REFERENCES llm_providers(id);
