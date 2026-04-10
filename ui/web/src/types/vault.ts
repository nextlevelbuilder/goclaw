/** Vault document in the Knowledge Vault registry. */
export interface VaultDocument {
  id: string;
  tenant_id: string;
  agent_id: string;
  team_id?: string;
  scope: "personal" | "team" | "shared";
  custom_scope?: string;
  path: string;
  title: string;
  doc_type: "context" | "memory" | "note" | "skill" | "episodic" | "media";
  content_hash: string;
  summary?: string;
  metadata: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

/** Directed link between two vault documents (wikilinks). */
export interface VaultLink {
  id: string;
  from_doc_id: string;
  to_doc_id: string;
  link_type: string;
  context: string;
  created_at: string;
}

/** Search result from vault hybrid search. */
export interface VaultSearchResult {
  document: VaultDocument;
  score: number;
  source: string;
}
