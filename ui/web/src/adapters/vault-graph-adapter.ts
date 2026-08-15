import Graph from "graphology";
import type { VaultDocument, VaultLink } from "@/types/vault";
import type { VaultGraphNode, VaultGraphEdge } from "@/types/graph-dto";
import { getNodeSize, truncateMiddle } from "@/components/graph/graph-utils";

// Colors per vault document type — matches DOC_TYPE_ICONS in vault-tree.tsx
// Light mode: Tailwind -600 variants for contrast on white/light backgrounds
// Dark mode: Tailwind -400 variants for visibility on dark backgrounds
export const VAULT_TYPE_COLORS_LIGHT: Record<string, string> = {
  context: "#2563eb",  // blue-600 (matches text-blue-600)
  memory: "#9333ea",   // purple-600 (matches text-purple-600)
  note: "#d97706",     // amber-600 (matches text-amber-600)
  skill: "#059669",    // emerald-600 (matches text-emerald-600)
  episodic: "#ea580c", // orange-600 (matches text-orange-600)
  media: "#e11d48",    // rose-600 (matches text-rose-600)
  document: "#0891b2", // cyan-600 (matches text-cyan-600)
};
export const VAULT_TYPE_COLORS_DARK: Record<string, string> = {
  context: "#60a5fa",  // blue-400 (matches dark:text-blue-400)
  memory: "#c084fc",   // purple-400 (matches dark:text-purple-400)
  note: "#fbbf24",     // amber-400 (matches dark:text-amber-400)
  skill: "#34d399",    // emerald-400 (matches dark:text-emerald-400)
  episodic: "#fb923c", // orange-400 (matches dark:text-orange-400)
  media: "#fb7185",    // rose-400 (matches dark:text-rose-400)
  document: "#22d3ee", // cyan-400 (matches dark:text-cyan-400)
};
const DEFAULT_COLOR_LIGHT = "#475569"; // slate-600
const DEFAULT_COLOR_DARK = "#94a3b8";  // slate-400

/** Get node color based on doc type and theme */
export function getVaultNodeColor(docType: string, isDark: boolean): string {
  const colors = isDark ? VAULT_TYPE_COLORS_DARK : VAULT_TYPE_COLORS_LIGHT;
  const fallback = isDark ? DEFAULT_COLOR_DARK : DEFAULT_COLOR_LIGHT;
  return colors[docType] ?? fallback;
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

/** Build graph from lightweight DTOs (degree pre-computed, no client loop). */
export function buildVaultGraphFromDTO(nodes: VaultGraphNode[], edges: VaultGraphEdge[]): Graph {
  const graph = new Graph({ multi: false, type: "directed" });
  const nodeIds = new Set(nodes.map((n) => n.id));

  for (const n of nodes) {
    graph.addNode(n.id, {
      label: truncateMiddle(n.t || n.p.split("/").pop() || n.id.slice(0, 8), 28),
      x: 0, y: 0,
      size: getNodeSize(n.deg, nodes.length),
      color: VAULT_TYPE_COLORS_LIGHT[n.dt] ?? DEFAULT_COLOR_LIGHT,
      docType: n.dt,
    });
  }

  for (const e of edges) {
    if (nodeIds.has(e.from) && nodeIds.has(e.to) && !graph.hasEdge(e.from, e.to)) {
      graph.addEdgeWithKey(e.id, e.from, e.to, {
        label: e.type, type: "curvedArrow",
        color: "#a1a1aa", size: 0.4,
      });
    }
  }

  return graph;
}

/** Build a Graphology graph from vault documents and their links. */
export function buildVaultGraph(
  documents: VaultDocument[],
  links: VaultLink[],
): Graph {
  const graph = new Graph({ multi: false, type: "directed" });
  const docIds = new Set(documents.map((d) => d.id));

  // Pre-compute degree map
  const degreeMap = new Map<string, number>();
  for (const link of links) {
    if (docIds.has(link.from_doc_id))
      degreeMap.set(link.from_doc_id, (degreeMap.get(link.from_doc_id) ?? 0) + 1);
    if (docIds.has(link.to_doc_id))
      degreeMap.set(link.to_doc_id, (degreeMap.get(link.to_doc_id) ?? 0) + 1);
  }

  // Add nodes (x/y assigned by container via circular layout before FA2)
  for (const doc of documents) {
    const degree = degreeMap.get(doc.id) ?? 0;
    const rawLabel = doc.title || doc.path.split("/").pop() || doc.id.slice(0, 8);
    graph.addNode(doc.id, {
      label: truncateMiddle(rawLabel, 28),
      x: 0,
      y: 0,
      size: getNodeSize(degree, documents.length),
      // Color set by nodeReducer based on theme; use light as initial fallback
      color: VAULT_TYPE_COLORS_LIGHT[doc.doc_type] ?? DEFAULT_COLOR_LIGHT,
      docType: doc.doc_type,
    });
  }

  // Add edges (only where both endpoints exist)
  for (const link of links) {
    if (docIds.has(link.from_doc_id) && docIds.has(link.to_doc_id)) {
      // Avoid duplicate edges for same source→target
      if (!graph.hasEdge(link.from_doc_id, link.to_doc_id)) {
        graph.addEdgeWithKey(link.id, link.from_doc_id, link.to_doc_id, {
          label: link.link_type,
          type: "curvedArrow",
          color: "#a1a1aa", // zinc-400, lighter gray
          size: 0.4,
        });
      }
    }
  }

  return graph;
}
