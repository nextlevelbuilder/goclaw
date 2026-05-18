-- Manual seed for the tuyettruong admin-agent + Telegram channel_instance.
-- Run after goclaw boots once (so the tools auto-register) and after you've
-- created the Telegram bot via @BotFather and obtained its token.
--
-- Replace the following placeholders before running:
--   <TENANT_UUID>            — your tenant id (SELECT id FROM tenants WHERE name='<your-tenant>')
--   <TELEGRAM_BOT_TOKEN>     — token from @BotFather
--   <PROVIDER_ID>            — anthropic/openai provider id (SELECT id FROM llm_providers ...)
--   <MODEL>                  — e.g. 'claude-sonnet-4-6'
--   <SYSTEM_PROMPT_TEXT>     — paste the full text from admin_agent_system_prompt.md (escape single quotes)
--
-- Tools whitelist for this agent: tt_* plus web_fetch + web_search. The two
-- web tools are needed by the photo-ingest flow — agent fetches the
-- manufacturer page (or searches for it) to pull clean product image URLs
-- before drafting. No `exec`, no shell — admin agent stays HTTP-only.

INSERT INTO agents (
  id, tenant_id, agent_key, provider_id, model,
  workspace, system_prompt,
  tools_config,
  other_config,
  status, created_at, updated_at
) VALUES (
  gen_random_uuid(),
  '<TENANT_UUID>',
  'tuyettruong-admin',
  '<PROVIDER_ID>',
  '<MODEL>',
  '/tmp/tuyettruong-admin-workspace',  -- filesystem sandbox (unused for HTTP-only agent)
  '<SYSTEM_PROMPT_TEXT>',
  jsonb_build_object(
    'allow', jsonb_build_array(
      'tt_product_search', 'tt_product_get',
      'tt_product_create', 'tt_product_update',
      'tt_variant_update', 'tt_product_delete',
      'tt_product_lookup_existing', 'tt_product_draft_from_extracted',
      'tt_order_list', 'tt_order_update_status',
      'web_fetch', 'web_search'
    )
  ),
  jsonb_build_object(
    'temperature', 0.2,
    'max_tool_iterations', 10
  ),
  'active',
  now(), now()
)
RETURNING id AS admin_agent_id;

-- After running the above, take the returned UUID and use it as <ADMIN_AGENT_ID> below.

INSERT INTO channel_instances (
  id, tenant_id, name, display_name, channel_type, agent_id,
  credentials, config, enabled, created_at, updated_at
) VALUES (
  gen_random_uuid(),
  '<TENANT_UUID>',
  'tuyettruong-admin-telegram',
  'Tuyết Trương Admin Bot',
  'telegram',
  '<ADMIN_AGENT_ID>',
  -- Encrypt with goclaw's secret tool before insert in production. Plaintext OK for dev.
  jsonb_build_object('token', '<TELEGRAM_BOT_TOKEN>'),
  jsonb_build_object(
    'allowed_user_ids', jsonb_build_array('<ANH_TELEGRAM_USER_ID>'),
    'default_policy', 'allowlist'
  ),
  true,
  now(), now()
);
