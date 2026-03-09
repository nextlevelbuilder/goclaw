CREATE TABLE kg_entities (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    external_id VARCHAR(255) NOT NULL,
    name        TEXT NOT NULL,
    entity_type VARCHAR(100) NOT NULL,
    description TEXT DEFAULT '',
    properties  JSONB DEFAULT '{}',
    source_id   VARCHAR(255) DEFAULT '',
    confidence  FLOAT NOT NULL DEFAULT 1.0,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id, external_id)
);

CREATE TABLE kg_relations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id         UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id          VARCHAR(255) NOT NULL DEFAULT '',
    source_entity_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    relation_type    VARCHAR(200) NOT NULL,
    target_entity_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    confidence       FLOAT NOT NULL DEFAULT 1.0,
    properties       JSONB DEFAULT '{}',
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id, source_entity_id, relation_type, target_entity_id)
);

CREATE INDEX idx_kg_entities_scope ON kg_entities(agent_id, user_id);
CREATE INDEX idx_kg_entities_type ON kg_entities(agent_id, user_id, entity_type);
CREATE INDEX idx_kg_relations_source ON kg_relations(source_entity_id, relation_type);
CREATE INDEX idx_kg_relations_target ON kg_relations(target_entity_id);
