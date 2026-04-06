-- Reverse KG temporal
DROP INDEX IF EXISTS idx_kg_relations_temporal;
DROP INDEX IF EXISTS idx_kg_relations_current;
DROP INDEX IF EXISTS idx_kg_entities_temporal;
DROP INDEX IF EXISTS idx_kg_entities_current;

ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_from;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_from;

-- Reverse tables
DROP TABLE IF EXISTS agent_evolution_suggestions;
DROP TABLE IF EXISTS agent_evolution_metrics;
DROP TABLE IF EXISTS episodic_summaries;
