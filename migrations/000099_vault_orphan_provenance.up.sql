-- Fix issue #1550: deleting an agent fails when another deleted owner already
-- orphaned a document at the same path.
--
-- vault_documents.agent_id is a FK with ON DELETE SET NULL. When it becomes NULL,
-- trg_vault_docs_agent_null_scope sets scope='shared'. The scope-consistency CHECK
-- (migration 000055/000056) requires scope='shared' to carry agent_id IS NULL AND
-- team_id IS NULL, and uq_vault_docs_agent_team_scope_path COALESCEs both nullable
-- columns to the same sentinel. Every orphan in a tenant therefore lands on the key
--   (tenant_id, sentinel, sentinel, 'shared', path)
-- which makes path unique per tenant for orphans, and the second delete that orphans
-- the same path aborts with SQLSTATE 23505 inside the FK's own UPDATE.
--
-- Orphans arrive from two directions, so both are covered here:
--   trg_vault_docs_agent_null_scope (000046) -- agent deleted, no team  -> 'shared'
--   vault_docs_team_null_scope_fix  (000089) -- team deleted, no agent  -> 'shared'
-- Before this migration, two deleted teams collide the same way two deleted agents do,
-- and a deleted agent collides with a deleted team.
--
-- The fix records which owner the document lost and includes that in the unique index,
-- so two orphans of different owners no longer share a key. path is left untouched:
-- nothing that already refers to a document path breaks, and re-pathing would be
-- ambiguous for a document that has been orphaned once already.

-- 1. Provenance. Deliberately one neutral column rather than a typed pair: it holds the
--    former agent id, the former team id, or -- for rows orphaned before this migration,
--    whose owner is no longer recorded anywhere -- the document's own id. No FK, because
--    the row it names is the one being deleted.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS orphaned_from_id uuid;

COMMENT ON COLUMN vault_documents.orphaned_from_id IS
    'The owner this document lost: former agent_id, former team_id, or the row id for '
    'documents orphaned before migration 000099. NULL while the document still has an owner. '
    'Part of uq_vault_docs_agent_team_scope_path so two orphans cannot share a key.';

-- 2. Backfill before the index is rebuilt, so it can be created UNIQUE in one step with no
--    cleanup pass. The current index already admits at most one scope='shared' row per
--    (tenant_id, path), so no two existing orphans collide; the row id is a deterministic,
--    row-derived discriminator. A shared reserved sentinel is deliberately avoided: it would
--    reproduce the shape being fixed here and could collide with a real provenance value later.
UPDATE vault_documents
   SET orphaned_from_id = id
 WHERE orphaned_from_id IS NULL
   AND agent_id IS NULL
   AND team_id IS NULL
   AND scope = 'shared';

-- 3. Rebuild the unique index with provenance appended. Appended rather than inserted so the
--    leading (tenant_id, agent_id, team_id, scope, path) prefix is unchanged and existing
--    lookups keep the same index support.
DROP INDEX IF EXISTS uq_vault_docs_agent_team_scope_path;
CREATE UNIQUE INDEX uq_vault_docs_agent_team_scope_path
    ON vault_documents (
        tenant_id,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
        COALESCE(team_id, '00000000-0000-0000-0000-000000000000'),
        scope,
        path,
        COALESCE(orphaned_from_id, '00000000-0000-0000-0000-000000000000')
    );

-- 4. Both triggers record the owner they drop. Conditions are otherwise unchanged: the agent
--    function keeps its 000046 condition, the team function keeps the OLD.scope='team' guard
--    and the personal/shared split from 000089, and provenance is written only on the branch
--    that reaches 'shared', since the other branch keeps an owner.
CREATE OR REPLACE FUNCTION vault_docs_agent_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.agent_id IS NULL AND OLD.agent_id IS NOT NULL AND NEW.team_id IS NULL THEN
        NEW.scope := 'shared';
        NEW.orphaned_from_id := OLD.agent_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL AND OLD.scope = 'team' THEN
        NEW.scope := CASE WHEN NEW.agent_id IS NOT NULL THEN 'personal' ELSE 'shared' END;
        IF NEW.agent_id IS NULL THEN
            NEW.orphaned_from_id := OLD.team_id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
