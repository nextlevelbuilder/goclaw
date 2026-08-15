-- Revert to the migration 000043 behavior: unconditionally coerce scope to
-- 'personal' when a team_id is cleared. NOTE: this reintroduces issue #1077 —
-- team-scoped docs (agent_id IS NULL) will again violate the scope-consistency
-- CHECK on team deletion.
CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL THEN
        NEW.scope := 'personal';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
