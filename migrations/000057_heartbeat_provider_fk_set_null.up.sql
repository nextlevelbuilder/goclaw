-- 1. Defensive orphan cleanup before changing FK semantics.
--    Old FK was RESTRICT so orphans should be impossible, but historical
--    schema drift could leave dangling provider_id references.
UPDATE agent_heartbeats
   SET provider_id = NULL
 WHERE provider_id IS NOT NULL
   AND provider_id NOT IN (SELECT id FROM llm_providers);

-- 2. Drop existing FK by lookup (handles auto-gen name drift).
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

-- 3. Re-add with ON DELETE SET NULL.
ALTER TABLE agent_heartbeats
  ADD CONSTRAINT agent_heartbeats_provider_id_fkey
    FOREIGN KEY (provider_id) REFERENCES llm_providers(id) ON DELETE SET NULL;
