-- Secret vault: agents may write secrets, but an agent-written entry starts
-- pending and cannot be used until a user approves it and confirms its
-- allowed domains. Existing rows are user-created and stay active.
--
-- created_by_agent is SET NULL on agent delete so the entry (and its pending
-- state) survives for the user to review or delete.

ALTER TABLE secret_vault_entries
    ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'pending')),
    ADD COLUMN created_via VARCHAR(16) NOT NULL DEFAULT 'user'
        CHECK (created_via IN ('user', 'agent')),
    ADD COLUMN created_by_agent UUID REFERENCES agents(id) ON DELETE SET NULL;

-- Per-agent pending cap lookup.
CREATE INDEX idx_secret_vault_entries_pending_agent
    ON secret_vault_entries(tenant_id, created_by_agent)
    WHERE status = 'pending';
