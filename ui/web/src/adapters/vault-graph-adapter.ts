import type { VaultDocument, VaultLink } from "@/types/vault";

// Colors per vault document type
export const VAULT_TYPE_COLORS: Record<string, string> = {
  context: "#3b82f6",  // blue
  memory: "#8b5cf6",   // purple
  note: "#eab308",     // yellow
  skill: "#22c55e",    // green
  episodic: "#f97316", // orange
  media: "#ef4444",    // red
};
const DEFAULT_COLOR = "#9ca3af";

export interface VaultGraphNode {
  id: string;
  title: string;
  docType: string;
  color: string;
  neighbors: Set<string>;
  linkIds: Set<string>;
  degree: number;
  x?: number;
  y?: number;
}

export interface VaultGraphLink {
  id: string;
  source: string;
  target: string;
  label: string;
}

export interface VaultGraphData {
  nodes: VaultGraphNode[];
  links: VaultGraphLink[];
}

/** Limit documents by degree centrality (highest-connected first). */
export function limitVaultDocsByDegree(
  docs: VaultDocument[],
  links: VaultLink[],
  nodeLimit: number,
): VaultDocument[] {
  if (docs.length <= nodeLimit) return docs;
  const ids = new Set(docs.map((d) => d.id));
  const deg = new Map<string, number>();
  for (const l of links) {
    if (ids.has(l.from_doc_id)) deg.set(l.from_doc_id, (deg.get(l.from_doc_id) ?? 0) + 1);
    if (ids.has(l.to_doc_id)) deg.set(l.to_doc_id, (deg.get(l.to_doc_id) ?? 0) + 1);
  }
  return [...docs].sort((a, b) => (deg.get(b.id) ?? 0) - (deg.get(a.id) ?? 0)).slice(0, nodeLimit);
}

/** Build graph data from vault documents and their links. */
export function buildVaultGraphData(
  documents: VaultDocument[],
  links: VaultLink[],
): VaultGraphData {
  const docIds = new Set(documents.map((d) => d.id));

  // Build degree map
  const degreeMap = new Map<string, number>();
  for (const link of links) {
    if (docIds.has(link.from_doc_id)) {
      degreeMap.set(link.from_doc_id, (degreeMap.get(link.from_doc_id) ?? 0) + 1);
    }
    if (docIds.has(link.to_doc_id)) {
      degreeMap.set(link.to_doc_id, (degreeMap.get(link.to_doc_id) ?? 0) + 1);
    }
  }

  const nodes: VaultGraphNode[] = documents.map((d) => ({
    id: d.id,
    title: d.title || d.path.split("/").pop() || d.id.slice(0, 8),
    docType: d.doc_type,
    color: VAULT_TYPE_COLORS[d.doc_type] ?? DEFAULT_COLOR,
    neighbors: new Set<string>(),
    linkIds: new Set<string>(),
    degree: degreeMap.get(d.id) ?? 0,
  }));

  const nodeMap = new Map(nodes.map((n) => [n.id, n]));

  // Only include links where both endpoints exist in our document set
  const graphLinks: VaultGraphLink[] = [];
  for (const link of links) {
    const src = nodeMap.get(link.from_doc_id);
    const tgt = nodeMap.get(link.to_doc_id);
    if (!src || !tgt) continue;
    src.neighbors.add(tgt.id);
    tgt.neighbors.add(src.id);
    src.linkIds.add(link.id);
    tgt.linkIds.add(link.id);
    graphLinks.push({
      id: link.id,
      source: link.from_doc_id,
      target: link.to_doc_id,
      label: link.link_type,
    });
  }

  return { nodes, links: graphLinks };
}
