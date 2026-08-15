/** Lightweight vault node from GET /v1/vault/graph (short keys match backend JSON) */
export interface VaultGraphNode {
  id: string;
  t: string;    // title
  p: string;    // path
  dt: string;   // doc_type
  deg: number;  // degree (pre-computed server-side)
}

/** Lightweight vault edge from GET /v1/vault/graph */
export interface VaultGraphEdge {
  id: string;
  from: string;
  to: string;
  type: string;
}

/** GET /v1/vault/graph response */
export interface VaultGraphResponse {
  nodes: VaultGraphNode[];
  edges: VaultGraphEdge[];
  total_nodes: number;
  total_edges: number;
}

/** Lightweight KG node from GET /v1/agents/{id}/kg/graph/compact */
export interface KGGraphNode {
  id: string;
  n: string;    // name
  t: string;    // entity_type
  c: number;    // confidence
}

/** Lightweight KG edge from GET /v1/agents/{id}/kg/graph/compact */
export interface KGGraphEdge {
  id: string;
  src: string;
  tgt: string;
  type: string;
}

/** GET /v1/agents/{id}/kg/graph/compact response */
export interface KGGraphCompactResponse {
  nodes: KGGraphNode[];
  edges: KGGraphEdge[];
  total_nodes: number;
  total_edges: number;
}
