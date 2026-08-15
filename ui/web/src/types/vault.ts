/** Vault document in the Knowledge Vault registry. */
export interface VaultDocument {
  id: string;
  tenant_id: string;
  agent_id?: string | null;
  team_id?: string;
  scope: "personal" | "team" | "shared";
  custom_scope?: string;
  path: string;
  /** Lowercased basename (`/absolute/path/foo.md` → `foo.md`). PG-generated / SQLite app-populated. */
  path_basename?: string;
  title: string;
  /**
   * - `document` added in Phase 01 for PDFs + office files (separate from `media`
   *   so enrichment can apply idempotent deterministic summaries).
   */
  doc_type:
    | "context"
    | "memory"
    | "note"
    | "skill"
    | "episodic"
    | "media"
    | "document";
  content_hash: string;
  summary?: string;
  metadata: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

/** Directed link between two vault documents (wikilinks + auto-linking). */
export interface VaultLink {
  id: string;
  from_doc_id: string;
  to_doc_id: string;
  /**
   * - Classify types: `reference`, `related`, `extends`, `depends_on`, `supersedes`, `contradicts`.
   * - Wiki: `wikilink`.
   * - Phase 04/05 auto-linking: `task_attachment`, `delegation_attachment`
   *   (invisible to classify wipe — never user-creatable via the link dialog).
   */
  link_type: string;
  context: string;
  /** Cleanup tracking metadata, e.g. `{ "source": "task:{uuid}" }` for auto-links. */
  metadata?: Record<string, unknown> | null;
  created_at: string;
}

/** Search result from vault hybrid search. */
export interface VaultSearchResult {
  document: VaultDocument;
  score: number;
  source: string;
}
