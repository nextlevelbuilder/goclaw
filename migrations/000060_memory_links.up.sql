-- Obsidian wikilinks → backlinks index.
-- Populated by internal/memory/wikilinks.go on each memory_document
-- index pass. Each row records "from_path links to to_path / to_basename
-- via a wikilink".
--
-- to_path is nullable: an unresolved link (target not yet in the vault)
-- still records to_basename so when the target is later added, a sweep
-- can re-resolve and backfill to_path. The (from_path, to_basename,
-- link_type, COALESCE(block_id, ''))) unique constraint dedupes
-- identical references (rare but happens when the same link appears
-- multiple times in one doc).

CREATE TABLE IF NOT EXISTS memory_links (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    from_path   TEXT NOT NULL,
    to_path     TEXT,
    to_basename TEXT NOT NULL,
    link_type   TEXT NOT NULL,             -- 'wiki' | 'embed' | 'block'
    section     TEXT,                       -- nullable: heading anchor for [[Note#Heading]]
    block_id    TEXT,                       -- nullable: block id for [[Note#^block]]
    display     TEXT,                       -- nullable: display alias for [[Note|Display]]
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_links_dedup
    ON memory_links(agent_id, COALESCE(user_id, ''), from_path, to_basename, link_type, COALESCE(block_id, ''));

CREATE INDEX IF NOT EXISTS idx_memory_links_to
    ON memory_links(agent_id, to_path)
    WHERE to_path IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_memory_links_from
    ON memory_links(agent_id, from_path);

CREATE INDEX IF NOT EXISTS idx_memory_links_basename
    ON memory_links(agent_id, to_basename);
