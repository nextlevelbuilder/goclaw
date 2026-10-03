-- Revert 000099: restore the trigger bodies and the index from before provenance existed.
--
-- Reverting reintroduces the #1550 collision, so any tenant holding two orphans of
-- different owners at one path has rows the old index cannot accept. Those rows are
-- re-pathed under a per-row prefix first, keeping the documents rather than deleting
-- them; the original path stays recoverable from the prefix.

CREATE OR REPLACE FUNCTION vault_docs_agent_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.agent_id IS NULL AND OLD.agent_id IS NOT NULL AND NEW.team_id IS NULL THEN
        NEW.scope := 'shared';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL AND OLD.scope = 'team' THEN
        NEW.scope := CASE WHEN NEW.agent_id IS NOT NULL THEN 'personal' ELSE 'shared' END;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Keep the newest orphan per (tenant_id, path) at its path; move the rest aside so the
-- old index can be created UNIQUE again.
WITH ranked AS (
    SELECT id,
           row_number() OVER (PARTITION BY tenant_id, path ORDER BY updated_at DESC, id) AS rn
      FROM vault_documents
     WHERE agent_id IS NULL AND team_id IS NULL AND scope = 'shared'
)
UPDATE vault_documents AS v
   SET path = '_orphan/' || v.id || '/' || v.path
  FROM ranked
 WHERE ranked.id = v.id AND ranked.rn > 1;

DROP INDEX IF EXISTS uq_vault_docs_agent_team_scope_path;
CREATE UNIQUE INDEX uq_vault_docs_agent_team_scope_path
    ON vault_documents (
        tenant_id,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
        COALESCE(team_id, '00000000-0000-0000-0000-000000000000'),
        scope,
        path
    );

ALTER TABLE vault_documents DROP COLUMN IF EXISTS orphaned_from_id;
