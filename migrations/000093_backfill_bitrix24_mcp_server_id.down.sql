-- Revert the backfill by dropping the mcp_server_id key from Bitrix24
-- channel_instances configs. The legacy mcp_server_name + mcp_base_url pair
-- was never touched by the up migration, so provisioning falls back to it
-- automatically once the new key is gone.
UPDATE channel_instances
   SET config = config - 'mcp_server_id',
       updated_at = NOW()
 WHERE channel_type = 'bitrix24'
   AND config ? 'mcp_server_id';
