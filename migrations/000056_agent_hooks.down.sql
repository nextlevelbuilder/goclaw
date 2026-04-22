-- Migration 000052 rollback: drop agent hooks tables in dependency order.
DROP TABLE IF EXISTS tenant_hook_budget;
DROP TABLE IF EXISTS hook_executions;
DROP TABLE IF EXISTS agent_hooks;
