-- Backfill channel_instances.config -> mcp_server_id for Bitrix24 channels that
-- currently store the legacy mcp_server_name. Resolves the name to a UUID via
-- mcp_servers (scoped to the channel agent's tenant) so the new MCP dropdown
-- and factory path (Phase 89) can drive lookups without touching the legacy
-- mcp_server_name / mcp_base_url pair.
--
-- Idempotent: only touches rows where config already carries the legacy
-- mcp_server_name AND does not yet carry mcp_server_id. Rows whose name does
-- not resolve to any mcp_servers row in the same tenant are left alone -- the
-- factory falls back to the legacy pair for them until an admin opens the
-- channel form and picks a server from the dropdown.
UPDATE channel_instances ci
   SET config = jsonb_set(
        COALESCE(ci.config, '{}'::jsonb),
        '{mcp_server_id}',
        to_jsonb(srv.id::text)
   ),
       updated_at = NOW()
  FROM mcp_servers srv, agents a
 WHERE ci.channel_type = 'bitrix24'
   AND ci.config ? 'mcp_server_name'
   AND NOT (ci.config ? 'mcp_server_id')
   AND ci.config->>'mcp_server_name' = srv.name
   AND ci.agent_id = a.id
   AND a.tenant_id = srv.tenant_id;
