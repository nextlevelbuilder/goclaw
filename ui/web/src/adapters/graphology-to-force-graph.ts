/**
 * Convert Graphology graph to react-force-graph format
 */
import type Graph from "graphology";

export interface ForceGraphNode {
  id: string;
  name: string;
  val: number;
  color?: string;
  type?: string;
  // 3D coordinates (computed by force simulation)
  x?: number;
  y?: number;
  z?: number;
}

export interface ForceGraphLink {
  source: string;
  target: string;
  color?: string;
}

export interface ForceGraphData {
  nodes: ForceGraphNode[];
  links: ForceGraphLink[];
}

/**
 * Convert Graphology graph to react-force-graph compatible format
 */
export function graphologyToForceGraph(graph: Graph): ForceGraphData {
  const nodes: ForceGraphNode[] = graph.nodes().map((id) => {
    const attrs = graph.getNodeAttributes(id);
    // Get type from docType (Vault) or entityType (KG)
    const nodeType = (attrs.docType || attrs.entityType || "default") as string;
    return {
      id,
      name: (attrs.label as string) || id,
      val: (attrs.size as number) || 5,
      color: attrs.color as string | undefined,
      type: nodeType,
    };
  });

  const links: ForceGraphLink[] = [];
  graph.forEachEdge((_edge, attrs, source, target) => {
    links.push({
      source,
      target,
      color: attrs.color as string | undefined,
    });
  });

  return { nodes, links };
}

/**
 * Get node color based on type and theme
 * Colors match VAULT_TYPE_COLORS and KG_TYPE_COLORS in their respective adapters
 */
export function getForceGraphNodeColor(type: string, isDark: boolean): string {
  const colors: Record<string, { light: string; dark: string }> = {
    // Vault document types (match vault-graph-adapter.ts)
    context: { light: "#2563eb", dark: "#60a5fa" },    // blue
    memory: { light: "#9333ea", dark: "#c084fc" },     // purple
    note: { light: "#d97706", dark: "#fbbf24" },       // amber
    skill: { light: "#059669", dark: "#34d399" },      // emerald
    episodic: { light: "#ea580c", dark: "#fb923c" },   // orange
    media: { light: "#e11d48", dark: "#fb7185" },      // rose
    document: { light: "#0891b2", dark: "#22d3ee" },   // cyan
    // KG entity types (match kg-graph.adapter.ts)
    person: { light: "#E85D24", dark: "#fb923c" },
    organization: { light: "#ef4444", dark: "#f87171" },
    project: { light: "#22c55e", dark: "#4ade80" },
    product: { light: "#f97316", dark: "#fb923c" },
    technology: { light: "#3b82f6", dark: "#60a5fa" },
    task: { light: "#f59e0b", dark: "#fbbf24" },
    event: { light: "#ec4899", dark: "#f472b6" },
    concept: { light: "#a78bfa", dark: "#c4b5fd" },
    location: { light: "#14b8a6", dark: "#2dd4bf" },
    // Default
    default: { light: "#475569", dark: "#94a3b8" },    // slate
  };

  const colorSet = colors[type?.toLowerCase()] || colors.default!;
  return isDark ? colorSet.dark : colorSet.light;
}
