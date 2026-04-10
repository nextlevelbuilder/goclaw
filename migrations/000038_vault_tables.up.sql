-- vault_documents: document registry for Knowledge Vault.
-- Metadata pointers: FS holds content, DB holds path + hash + embedding + links.
CREATE TABLE vault_documents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope        TEXT NOT NULL DEFAULT 'personal',
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    embedding    vector(1536),
    metadata     JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, scope, path)
);

CREATE INDEX idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX idx_vault_docs_embedding ON vault_documents
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

-- FTS on title + path for keyword search.
ALTER TABLE vault_documents ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(title,'') || ' ' || coalesce(path,''))) STORED;
CREATE INDEX idx_vault_docs_tsv ON vault_documents USING gin(tsv);

-- vault_links: bidirectional links between docs (wikilinks).
CREATE TABLE vault_links (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_doc_id UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    to_doc_id   UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    link_type   TEXT NOT NULL DEFAULT 'wikilink',
    context     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(from_doc_id, to_doc_id, link_type)
);

CREATE INDEX idx_vault_links_from ON vault_links(from_doc_id);
CREATE INDEX idx_vault_links_to ON vault_links(to_doc_id);

-- vault_versions: v3.1 prep (empty for now, schema only).
CREATE TABLE vault_versions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doc_id     UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    version    INT NOT NULL DEFAULT 1,
    content    TEXT NOT NULL DEFAULT '',
    changed_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(doc_id, version)
);
