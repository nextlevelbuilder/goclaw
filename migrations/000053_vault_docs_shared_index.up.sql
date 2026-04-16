CREATE INDEX IF NOT EXISTS idx_vault_docs_shared
    ON vault_documents(tenant_id)
    WHERE agent_id IS NULL;
