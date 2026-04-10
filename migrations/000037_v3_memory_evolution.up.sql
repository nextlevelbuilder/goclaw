-- V3 Core: Memory, Evolution, KG temporal
-- Migration 000037

-- Episodic summaries (Tier 2 memory)
CREATE TABLE episodic_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,

    summary      TEXT NOT NULL,
    l0_abstract  TEXT NOT NULL DEFAULT '',
    key_topics   TEXT[] DEFAULT '{}',
    embedding    vector(1536),
    source_type  TEXT NOT NULL DEFAULT 'session',
    source_id    TEXT,
    turn_count  INT NOT NULL DEFAULT 0,
    token_count INT NOT NULL DEFAULT 0,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ
);

CREATE INDEX idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE UNIQUE INDEX idx_episodic_source_dedup ON episodic_summaries(agent_id, user_id, source_id)
    WHERE source_id IS NOT NULL;
CREATE INDEX idx_episodic_tsv ON episodic_summaries USING GIN(to_tsvector('simple', summary));
CREATE INDEX idx_episodic_vec ON episodic_summaries USING hnsw(embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX idx_episodic_expires ON episodic_summaries(expires_at) WHERE expires_at IS NOT NULL;

-- Evolution metrics (Stage 1 self-evolution)
CREATE TABLE agent_evolution_metrics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,

    metric_type TEXT NOT NULL,
    metric_key  TEXT NOT NULL,
    value       JSONB NOT NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- Evolution suggestions (Stage 2 self-evolution)
CREATE TABLE agent_evolution_suggestions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    suggestion_type TEXT NOT NULL,
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    parameters      JSONB,

    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);

-- KG temporal validity windows
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

CREATE INDEX idx_kg_entities_current ON kg_entities(agent_id, user_id)
    WHERE valid_until IS NULL;
CREATE INDEX idx_kg_entities_temporal ON kg_entities(agent_id, user_id, valid_from, valid_until);

CREATE INDEX idx_kg_relations_current ON kg_relations(agent_id, user_id)
    WHERE valid_until IS NULL;
CREATE INDEX idx_kg_relations_temporal ON kg_relations(agent_id, user_id, valid_from, valid_until);

-- Promote well-known fields from agents.other_config JSONB to dedicated columns

-- 7 scalar columns
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS emoji TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS agent_description TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS thinking_level TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS max_tokens INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS self_evolve BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS skill_evolve BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS skill_nudge_interval INT NOT NULL DEFAULT 0;

-- 5 nested JSONB columns (structs that stay JSON-shaped)
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS reasoning_config JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS workspace_sharing JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS chatgpt_oauth_routing JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS shell_deny_groups JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS kg_dedup_config JSONB NOT NULL DEFAULT '{}';

-- Backfill from other_config
UPDATE agents SET
  emoji = COALESCE(other_config->>'emoji', ''),
  agent_description = COALESCE(other_config->>'description', ''),
  thinking_level = COALESCE(other_config->>'thinking_level', ''),
  max_tokens = COALESCE((other_config->>'max_tokens')::int, 0),
  self_evolve = COALESCE((other_config->>'self_evolve')::boolean, false),
  skill_evolve = COALESCE((other_config->>'skill_evolve')::boolean, false),
  skill_nudge_interval = COALESCE((other_config->>'skill_nudge_interval')::int, 0),
  reasoning_config = COALESCE(other_config->'reasoning', '{}'),
  workspace_sharing = COALESCE(other_config->'workspace_sharing', '{}'),
  chatgpt_oauth_routing = COALESCE(other_config->'chatgpt_oauth_routing', '{}'),
  shell_deny_groups = COALESCE(other_config->'shell_deny_groups', '{}'),
  kg_dedup_config = COALESCE(other_config->'kg_dedup_config', '{}')
WHERE other_config != '{}' AND other_config IS NOT NULL;

-- Clean promoted keys from other_config
UPDATE agents SET other_config = other_config
  - 'emoji' - 'description' - 'thinking_level' - 'max_tokens'
  - 'self_evolve' - 'skill_evolve' - 'skill_nudge_interval'
  - 'reasoning' - 'workspace_sharing' - 'chatgpt_oauth_routing'
  - 'shell_deny_groups' - 'kg_dedup_config';
