DROP INDEX IF EXISTS idx_kg_entity_vec;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS embedding;
