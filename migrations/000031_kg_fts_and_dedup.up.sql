-- tsvector full-text search for KG entities (replaces ILIKE)
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', name || ' ' || COALESCE(description, ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_kg_entities_tsv ON kg_entities USING GIN (tsv);

-- Dedup candidates table for entity deduplication review
CREATE TABLE IF NOT EXISTS kg_dedup_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    entity_a_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    entity_b_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    similarity FLOAT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(entity_a_id, entity_b_id)
);

CREATE INDEX IF NOT EXISTS idx_kg_dedup_agent ON kg_dedup_candidates(agent_id, status);
