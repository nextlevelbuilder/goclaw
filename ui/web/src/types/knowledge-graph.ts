export interface KGEntity {
  id: string;
  agent_id: string;
  user_id?: string;
  external_id: string;
  name: string;
  entity_type: string;
  description?: string;
  properties?: Record<string, string>;
  source_id?: string;
  confidence: number;
  created_at: number;
  updated_at: number;
}

export interface KGRelation {
  id: string;
  agent_id: string;
  user_id?: string;
  source_entity_id: string;
  relation_type: string;
  target_entity_id: string;
  confidence: number;
  properties?: Record<string, string>;
  created_at: number;
}

export interface KGTraversalResult {
  entity: KGEntity;
  depth: number;
  path: string[];
  via: string;
}

export interface KGStats {
  entity_count: number;
  relation_count: number;
  entity_types: Record<string, number>;
  user_ids?: string[];
}

export interface KGDedupCandidate {
  id: string;
  entity_a: KGEntity;
  entity_b: KGEntity;
  similarity: number;
  status: string;
  created_at: number;
}
