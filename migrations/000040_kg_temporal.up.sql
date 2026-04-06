ALTER TABLE kg_entities
    ADD COLUMN valid_from  TIMESTAMPTZ DEFAULT NOW(),
    ADD COLUMN valid_until TIMESTAMPTZ;

ALTER TABLE kg_relations
    ADD COLUMN valid_from  TIMESTAMPTZ DEFAULT NOW(),
    ADD COLUMN valid_until TIMESTAMPTZ;

-- Backfill: valid_from = created_at (bigint epoch -> timestamptz)
UPDATE kg_entities SET valid_from = to_timestamp(created_at) WHERE valid_from IS NULL;
UPDATE kg_relations SET valid_from = to_timestamp(created_at) WHERE valid_from IS NULL;

-- Partial indexes for current facts (most common query pattern)
CREATE INDEX idx_kg_entities_current ON kg_entities(agent_id, user_id) WHERE valid_until IS NULL;
CREATE INDEX idx_kg_relations_current ON kg_relations(agent_id, user_id) WHERE valid_until IS NULL;
