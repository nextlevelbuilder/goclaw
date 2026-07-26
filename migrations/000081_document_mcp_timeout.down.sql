-- 000081_document_mcp_timeout.down.sql
--
-- Revert document-mcp's bridge timeout back to the 60s default.
UPDATE mcp_servers
SET timeout_sec = 60,
    updated_at  = NOW()
WHERE name = 'document-mcp'
  AND tenant_id IS NULL;
