CREATE TABLE episodic_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key VARCHAR(500) NOT NULL,
    summary       TEXT NOT NULL,
    key_entities  JSONB DEFAULT '[]',
    key_topics    JSONB DEFAULT '[]',
    turn_count    INT NOT NULL DEFAULT 0,
    token_count   INT NOT NULL DEFAULT 0,
    source_type   VARCHAR(50) NOT NULL DEFAULT 'session',
    embedding     vector(1536),
    tsv           tsvector GENERATED ALWAYS AS (to_tsvector('simple', summary)) STORED,
    l0_abstract   TEXT NOT NULL DEFAULT '',
    source_id     VARCHAR(255) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    expires_at    TIMESTAMPTZ,
    UNIQUE(agent_id, user_id, source_id)
);

CREATE INDEX idx_episodic_scope ON episodic_summaries(agent_id, user_id);
CREATE INDEX idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE INDEX idx_episodic_created ON episodic_summaries(agent_id, user_id, created_at DESC);
CREATE INDEX idx_episodic_expires ON episodic_summaries(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_episodic_tsv ON episodic_summaries USING gin(tsv);
CREATE INDEX idx_episodic_embedding ON episodic_summaries
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 50);

-- Clear all agent_links. Teams use agent_team_members directly;
-- delegate tool (v3) will use explicit links created via API.
TRUNCATE agent_links;
