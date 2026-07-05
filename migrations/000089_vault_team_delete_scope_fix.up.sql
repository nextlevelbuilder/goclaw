-- Fix issue #1077: deleting a team fails when it owns team-scoped vault docs.
--
-- vault_documents.team_id is a FK with ON DELETE SET NULL. When team_id becomes
-- NULL, the trg_vault_docs_team_null_scope trigger auto-corrects scope. The original
-- function (migration 000043) set scope='personal' unconditionally, but team-scoped
-- docs have agent_id IS NULL, so the resulting (scope='personal', agent_id IS NULL)
-- row violates vault_documents_scope_consistency (migration 000055/000056) and aborts
-- the whole delete with SQLSTATE 23514.
--
-- Pick a scope that keeps the ownership invariant, and only rewrite genuinely
-- team-scoped rows so scope='custom' docs that merely carry a team_id are left intact:
--   agent_id IS NOT NULL -> 'personal' (defensive: tolerate legacy dirty rows)
--   agent_id IS NULL     -> 'shared'   (normal team docs; the common case)
CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL AND OLD.scope = 'team' THEN
        NEW.scope := CASE WHEN NEW.agent_id IS NOT NULL THEN 'personal' ELSE 'shared' END;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
