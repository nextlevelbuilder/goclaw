-- Scope/ownership invariant for vault_documents.
-- personal → agent_id NOT NULL, team_id NULL
-- team     → team_id NOT NULL, agent_id NULL
-- shared   → both NULL
-- custom   → no constraint (user-defined scopes)
ALTER TABLE vault_documents
    ADD CONSTRAINT vault_documents_scope_consistency
    CHECK (
        (scope = 'personal' AND agent_id IS NOT NULL AND team_id IS NULL) OR
        (scope = 'team'     AND team_id  IS NOT NULL AND agent_id IS NULL) OR
        (scope = 'shared'   AND agent_id IS NULL     AND team_id  IS NULL) OR
        scope = 'custom'
    ) NOT VALID;

-- Ops step (run after audit cleanup):
--   ALTER TABLE vault_documents VALIDATE CONSTRAINT vault_documents_scope_consistency;
