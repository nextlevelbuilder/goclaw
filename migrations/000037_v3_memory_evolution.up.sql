-- V3 Core: Memory, Evolution, KG temporal
-- Migration 000037

-- Episodic summaries (Tier 2 memory)
CREATE TABLE episodic_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,

    summary     TEXT NOT NULL,
    key_topics  TEXT[] DEFAULT '{}',
    embedding   vector(1536),
    source_type TEXT NOT NULL DEFAULT 'session',
    source_id   TEXT,
    turn_count  INT NOT NULL DEFAULT 0,
    token_count INT NOT NULL DEFAULT 0,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ
);

CREATE INDEX idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE INDEX idx_episodic_source ON episodic_summaries(agent_id, source_id);
CREATE INDEX idx_episodic_tsv ON episodic_summaries USING GIN(to_tsvector('simple', summary));
CREATE INDEX idx_episodic_vec ON episodic_summaries USING hnsw(embedding vector_cosine_ops);
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
