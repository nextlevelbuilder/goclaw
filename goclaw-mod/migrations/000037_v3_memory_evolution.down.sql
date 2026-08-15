-- Reverse promoted other_config columns
UPDATE agents SET other_config = other_config
  || jsonb_build_object(
    'emoji', emoji,
    'description', agent_description,
    'thinking_level', thinking_level,
    'max_tokens', max_tokens,
    'self_evolve', self_evolve,
    'skill_evolve', skill_evolve,
    'skill_nudge_interval', skill_nudge_interval,
    'reasoning', reasoning_config,
    'workspace_sharing', workspace_sharing,
    'chatgpt_oauth_routing', chatgpt_oauth_routing,
    'shell_deny_groups', shell_deny_groups,
    'kg_dedup_config', kg_dedup_config
  );

ALTER TABLE agents
  DROP COLUMN IF EXISTS emoji,
  DROP COLUMN IF EXISTS agent_description,
  DROP COLUMN IF EXISTS thinking_level,
  DROP COLUMN IF EXISTS max_tokens,
  DROP COLUMN IF EXISTS self_evolve,
  DROP COLUMN IF EXISTS skill_evolve,
  DROP COLUMN IF EXISTS skill_nudge_interval,
  DROP COLUMN IF EXISTS reasoning_config,
  DROP COLUMN IF EXISTS workspace_sharing,
  DROP COLUMN IF EXISTS chatgpt_oauth_routing,
  DROP COLUMN IF EXISTS shell_deny_groups,
  DROP COLUMN IF EXISTS kg_dedup_config;

-- Reverse KG temporal
DROP INDEX IF EXISTS idx_kg_relations_temporal;
DROP INDEX IF EXISTS idx_kg_relations_current;
DROP INDEX IF EXISTS idx_kg_entities_temporal;
DROP INDEX IF EXISTS idx_kg_entities_current;

ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_from;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_from;

-- Reverse tables
DROP TABLE IF EXISTS agent_evolution_suggestions;
DROP TABLE IF EXISTS agent_evolution_metrics;
DROP TABLE IF EXISTS episodic_summaries;
