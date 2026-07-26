-- 000081_document_mcp_timeout.up.sql
--
-- Raise the document-mcp MCP-server bridge timeout from the default 60s to 600s.
--
-- document-mcp's generate_video tool runs Veo on Vertex AI, which blocks for
-- ~30s–3min inside a single tools/call while llm-service polls the long-running
-- operation. The default 60s bridge-tool timeout (internal/mcp/bridge_tool.go)
-- kills the call mid-generation, so the agent sees "MCP tool timeout after 60s"
-- even though the video succeeds downstream. Same reasoning as composio-mcp's
-- timeout_sec=600 (auth-proxy provisionSameStackMCP) for its minutes-long bulk
-- tools.
--
-- Global row only (tenant_id IS NULL); document-mcp is a shared server-side
-- sidecar with no per-tenant rows and no SQLite/desktop presence. Idempotent.
UPDATE mcp_servers
SET timeout_sec = 600,
    updated_at  = NOW()
WHERE name = 'document-mcp'
  AND tenant_id IS NULL
  AND timeout_sec < 600;
