DROP INDEX IF EXISTS idx_kg_entities_current;
DROP INDEX IF EXISTS idx_kg_relations_current;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_from, DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_from, DROP COLUMN IF EXISTS valid_until;
