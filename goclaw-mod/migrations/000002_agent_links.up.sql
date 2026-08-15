-- ============================================================
-- Agent frontmatter (short expertise/capability summary for delegation + UI display).
-- NOTE: This is NOT the same as other_config.description which is the
-- LLM summoning prompt used to initialize the agent's identity.
-- ============================================================

ALTER TABLE agents ADD COLUMN IF NOT EXISTS frontmatter TEXT;

-- Search support: FTS (tsvector) + semantic (pgvector) for agent discovery.
-- Pattern follows skills table: hybrid BM25 + cosine similarity.
-- tsv is auto-generated from display_name + frontmatter.
-- embedding is populated on create/update when an embedding provider is available.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', COALESCE(display_name, '') || ' ' || COALESCE(frontmatter, ''))) STORED;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS embedding vector(1536);
CREATE INDEX IF NOT EXISTS idx_agents_tsv ON agents USING GIN(tsv);
CREATE INDEX IF NOT EXISTS idx_agents_embedding ON agents USING hnsw(embedding vector_cosine_ops);

-- ============================================================
-- Agent Links (inter-agent delegation permissions)
-- ============================================================

CREATE TABLE agent_links (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    source_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    target_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    direction       VARCHAR(20) NOT NULL DEFAULT 'outbound',
    description     TEXT,
    max_concurrent  INT NOT NULL DEFAULT 3,
    settings        JSONB NOT NULL DEFAULT '{}',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by      VARCHAR(255) NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(source_agent_id, target_agent_id),
    CHECK (source_agent_id != target_agent_id)
);

CREATE INDEX idx_agent_links_source ON agent_links(source_agent_id) WHERE status = 'active';
CREATE INDEX idx_agent_links_target ON agent_links(target_agent_id) WHERE status = 'active';

-- ============================================================
-- Linked traces for delegation: parent_trace_id on traces table
-- allows navigating between caller and delegate traces.
-- ============================================================

ALTER TABLE traces ADD COLUMN IF NOT EXISTS parent_trace_id UUID;
CREATE INDEX IF NOT EXISTS idx_traces_parent ON traces(parent_trace_id) WHERE parent_trace_id IS NOT NULL;
