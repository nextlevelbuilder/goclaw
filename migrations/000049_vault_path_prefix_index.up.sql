CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_vault_docs_path_prefix
    ON vault_documents (tenant_id, path text_pattern_ops);
