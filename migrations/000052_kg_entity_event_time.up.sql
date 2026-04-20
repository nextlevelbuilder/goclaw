ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS event_time TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_kg_entities_event_time
    ON kg_entities(agent_id, user_id, event_time) WHERE event_time IS NOT NULL;
