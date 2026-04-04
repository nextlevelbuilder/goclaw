import type { KGEntity, KGRelation } from "@/types/knowledge-graph";

// Solid colors per entity type — used for both circle fill and glow
export const KG_TYPE_COLORS: Record<string, string> = {
  person: "#E85D24", organization: "#ef4444", project: "#22c55e",
  product: "#f97316", technology: "#3b82f6", task: "#f59e0b",
  event: "#ec4899", document: "#8b5cf6", concept: "#a78bfa", location: "#14b8a6",
};
export const KG_DEFAULT_COLOR = "#9ca3af";

export interface GraphNode {
  id: string;
  name: string;
  entityType: string;
  color: string;
  neighbors: Set<string>;
  linkIds: Set<string>;
  degree: number;
  // Force-graph adds these at runtime
  x?: number;
  y?: number;
}

export interface GraphLink {
  id: string;
  source: string;
  target: string;
  label: string;
}

export interface GraphData {
  nodes: GraphNode[];
  links: GraphLink[];
}

/** Compute degree (edge count) for each entity id. */
export function computeDegreeMap(entities: KGEntity[], relations: KGRelation[]): Map<string, number> {
  const deg = new Map<string, number>();
  const ids = new Set(entities.map((e) => e.id));
  for (const r of relations) {
    if (ids.has(r.source_entity_id)) deg.set(r.source_entity_id, (deg.get(r.source_entity_id) ?? 0) + 1);
    if (ids.has(r.target_entity_id)) deg.set(r.target_entity_id, (deg.get(r.target_entity_id) ?? 0) + 1);
  }
  return deg;
}

/**
 * Transform KGEntity[] + KGRelation[] into react-force-graph GraphData.
 * Builds nodes with color/degree, links with neighbor/linkId sets populated.
 */
export function buildGraphData(entities: KGEntity[], allRelations: KGRelation[]): GraphData {
  const entityIds = new Set(entities.map((e) => e.id));
  const degreeMap = computeDegreeMap(entities, allRelations);

  const nodes: GraphNode[] = entities.map((e) => ({
    id: e.id,
    name: e.name,
    entityType: e.entity_type,
    color: KG_TYPE_COLORS[e.entity_type] ?? KG_DEFAULT_COLOR,
    neighbors: new Set<string>(),
    linkIds: new Set<string>(),
    degree: degreeMap.get(e.id) ?? 0,
  }));

  const nodeMap = new Map(nodes.map((n) => [n.id, n]));

  const links: GraphLink[] = allRelations
    .filter((r) => entityIds.has(r.source_entity_id) && entityIds.has(r.target_entity_id))
    .map((r) => {
      nodeMap.get(r.source_entity_id)?.neighbors.add(r.target_entity_id);
      nodeMap.get(r.target_entity_id)?.neighbors.add(r.source_entity_id);
      nodeMap.get(r.source_entity_id)?.linkIds.add(r.id);
      nodeMap.get(r.target_entity_id)?.linkIds.add(r.id);
      return {
        id: r.id,
        source: r.source_entity_id,
        target: r.target_entity_id,
        label: r.relation_type.replace(/_/g, " "),
      };
    });

  return { nodes, links };
}

/**
 * Limit entities to nodeLimit by degree centrality (highest-degree first).
 */
export function limitEntitiesByDegree(
  allEntities: KGEntity[],
  allRelations: KGRelation[],
  nodeLimit: number,
): KGEntity[] {
  if (allEntities.length <= nodeLimit) return allEntities;
  const deg = computeDegreeMap(allEntities, allRelations);
  return [...allEntities].sort((a, b) => (deg.get(b.id) ?? 0) - (deg.get(a.id) ?? 0)).slice(0, nodeLimit);
}
