DROP INDEX IF EXISTS idx_kg_entities_event_time;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS event_time;
