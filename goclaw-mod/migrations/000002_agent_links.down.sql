DROP INDEX IF EXISTS idx_traces_parent;
ALTER TABLE traces DROP COLUMN IF EXISTS parent_trace_id;
DROP TABLE IF EXISTS agent_links;
DROP INDEX IF EXISTS idx_agents_embedding;
DROP INDEX IF EXISTS idx_agents_tsv;
ALTER TABLE agents DROP COLUMN IF EXISTS embedding;
ALTER TABLE agents DROP COLUMN IF EXISTS tsv;
ALTER TABLE agents DROP COLUMN IF EXISTS frontmatter;
