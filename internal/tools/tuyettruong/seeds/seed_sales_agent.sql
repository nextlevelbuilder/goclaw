-- Manual seed for the tuyettruong sales-agent + Zalo Personal channel_instance.
-- Run after the admin-agent is up and you've prepared a dedicated Zalo account
-- (with cookies/imei exported via zcago helper).
--
-- Replace placeholders:
--   <TENANT_UUID>            — your tenant id
--   <PROVIDER_ID>            — anthropic/openai provider id
--   <MODEL>                  — e.g. 'claude-sonnet-4-6'
--   <SYSTEM_PROMPT_TEXT>     — paste from sales_agent_system_prompt.md (escape quotes)
--   <ZALO_CREDENTIALS_JSON>  — output of zcago login for the bot account
--
-- Tools whitelist for sales-agent is intentionally narrow.

INSERT INTO agents (
  id, tenant_id, agent_key, provider_id, model,
  workspace, system_prompt,
  tools_config,
  other_config,
  status, created_at, updated_at
) VALUES (
  gen_random_uuid(),
  '<TENANT_UUID>',
  'tuyettruong-sales',
  '<PROVIDER_ID>',
  '<MODEL>',
  '/tmp/tuyettruong-sales-workspace',
  '<SYSTEM_PROMPT_TEXT>',
  jsonb_build_object(
    'allow', jsonb_build_array(
      'sales_product_search',
      'quote_add_item', 'quote_remove_item', 'quote_view',
      'quote_set_customer', 'quote_finalize', 'quote_clear',
      'order_place', 'order_customer_claimed_paid',
      'order_lookup', 'notify_admin'
    )
  ),
  jsonb_build_object(
    'temperature', 0.3,
    'max_tool_iterations', 12
  ),
  'active',
  now(), now()
)
RETURNING id AS sales_agent_id;

-- After above, take returned UUID as <SALES_AGENT_ID>.

INSERT INTO channel_instances (
  id, tenant_id, name, display_name, channel_type, agent_id,
  credentials, config, enabled, created_at, updated_at
) VALUES (
  gen_random_uuid(),
  '<TENANT_UUID>',
  'tuyettruong-sales-zalo-personal',
  'Tuyết Trương Sales',
  'zalo_personal',
  '<SALES_AGENT_ID>',
  -- Plaintext for dev; encrypt in production. Output from zcago login flow.
  '<ZALO_CREDENTIALS_JSON>'::jsonb,
  jsonb_build_object(
    'default_policy', 'allowlist',  -- restrict to test users first; flip to 'open' after smoke test
    'allowed_users', jsonb_build_array('<TEST_USER_ZALO_ID>')
  ),
  true,
  now(), now()
);

-- For Telegram smoke testing without Zalo, you can also bind sales-agent to a
-- second Telegram bot:
-- INSERT INTO channel_instances (...) VALUES (..., 'telegram', '<SALES_AGENT_ID>',
--   jsonb_build_object('token', '<SALES_TG_TOKEN>'), ...);
