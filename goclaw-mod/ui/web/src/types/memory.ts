export interface MemoryDocument {
  path: string;
  hash: string;
  agent_id?: string;
  user_id?: string;
  updated_at: number;
}

export interface MemoryDocumentDetail {
  path: string;
  content: string;
  hash: string;
  user_id?: string;
  chunk_count: number;
  embedded_count: number;
  created_at: number;
  updated_at: number;
}

export interface MemoryChunk {
  id: string;
  start_line: number;
  end_line: number;
  text_preview: string;
  has_embedding: boolean;
}

export interface MemorySearchResult {
  path: string;
  start_line: number;
  end_line: number;
  score: number;
  snippet: string;
  scope?: string;
}

/** Tier 2 episodic memory summary. */
export interface EpisodicSummary {
  id: string;
  agent_id: string;
  user_id: string;
  session_key: string;
  summary: string;
  key_topics: string[];
  l0_abstract: string;
  source_type: "session" | "v2_daily" | "manual";
  turn_count: number;
  token_count: number;
  created_at: string;
  expires_at: string | null;
}

/** Episodic search result. */
export interface EpisodicSearchResult {
  episodic_id: string;
  l0_abstract: string;
  score: number;
  created_at: string;
  session_key: string;
}
