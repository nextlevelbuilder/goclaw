-- Reverse of 000001_init_schema.up.sql
-- Drop in reverse dependency order

-- 14. Group File Writers
DROP TABLE IF EXISTS group_file_writers CASCADE;

-- 13. Config Secrets
DROP TABLE IF EXISTS config_secrets CASCADE;

-- 12. Channel Instances
DROP TABLE IF EXISTS channel_instances CASCADE;

-- 11. Custom Tools
DROP TABLE IF EXISTS custom_tools CASCADE;

-- 10. MCP Servers
DROP TABLE IF EXISTS mcp_access_requests CASCADE;
DROP TABLE IF EXISTS mcp_user_grants CASCADE;
DROP TABLE IF EXISTS mcp_agent_grants CASCADE;
DROP TABLE IF EXISTS mcp_servers CASCADE;

-- 9. Tracing
DROP TABLE IF EXISTS spans CASCADE;
DROP TABLE IF EXISTS traces CASCADE;

-- 8. Pairing
DROP TABLE IF EXISTS paired_devices CASCADE;
DROP TABLE IF EXISTS pairing_requests CASCADE;

-- 7. Cron
DROP TABLE IF EXISTS cron_run_logs CASCADE;
DROP TABLE IF EXISTS cron_jobs CASCADE;

-- 6. Skills
DROP TABLE IF EXISTS skill_user_grants CASCADE;
DROP TABLE IF EXISTS skill_agent_grants CASCADE;
DROP TABLE IF EXISTS skills CASCADE;

-- 5. Memory
DROP TABLE IF EXISTS embedding_cache CASCADE;
DROP TABLE IF EXISTS memory_chunks CASCADE;
DROP TABLE IF EXISTS memory_documents CASCADE;

-- 4. Sessions
DROP TABLE IF EXISTS sessions CASCADE;

-- 3. Context Files & User Profiles
DROP TABLE IF EXISTS user_agent_profiles CASCADE;
DROP TABLE IF EXISTS user_agent_overrides CASCADE;
DROP TABLE IF EXISTS user_context_files CASCADE;
DROP TABLE IF EXISTS agent_context_files CASCADE;

-- 2. Agents
DROP TABLE IF EXISTS agent_shares CASCADE;
DROP TABLE IF EXISTS agents CASCADE;

-- 1. LLM
DROP TABLE IF EXISTS llm_providers CASCADE;

-- Functions
DROP FUNCTION IF EXISTS uuid_generate_v7();

-- Extensions (only drop if safe — skip in production)
-- DROP EXTENSION IF EXISTS "vector";
-- DROP EXTENSION IF EXISTS "pgcrypto";
