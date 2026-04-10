import Graph from "graphology";
import type { KGEntity, KGRelation } from "@/types/knowledge-graph";
import { getNodeSize, truncateMiddle } from "@/components/graph/graph-utils";

// Solid colors per entity type
export const KG_TYPE_COLORS: Record<string, string> = {
  person: "#E85D24", organization: "#ef4444", project: "#22c55e",
  product: "#f97316", technology: "#3b82f6", task: "#f59e0b",
  event: "#ec4899", document: "#8b5cf6", concept: "#a78bfa", location: "#14b8a6",
};
export const KG_DEFAULT_COLOR = "#9ca3af";

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

/** Build a Graphology graph from KG entities and relations. */
export function buildKGGraph(entities: KGEntity[], allRelations: KGRelation[]): Graph {
  const graph = new Graph({ multi: false, type: "directed" });
  const entityIds = new Set(entities.map((e) => e.id));
  const degreeMap = computeDegreeMap(entities, allRelations);

  // Add nodes (x/y assigned by container via circular layout before FA2)
  for (const e of entities) {
    if (!graph.hasNode(e.id)) {
      const degree = degreeMap.get(e.id) ?? 0;
      graph.addNode(e.id, {
        label: truncateMiddle(e.name, 28),
        x: 0,
        y: 0,
        size: getNodeSize(degree),
        color: KG_TYPE_COLORS[e.entity_type] ?? KG_DEFAULT_COLOR,
        entityType: e.entity_type,
      });
    }
  }

  // Add edges (straight arrows for KG)
  for (const r of allRelations) {
    if (entityIds.has(r.source_entity_id) && entityIds.has(r.target_entity_id)) {
      if (!graph.hasEdge(r.source_entity_id, r.target_entity_id)) {
        graph.addEdgeWithKey(r.id, r.source_entity_id, r.target_entity_id, {
          label: r.relation_type.replace(/_/g, " "),
          type: "curvedArrow",
        });
      }
    }
  }

  return graph;
}

/** Limit entities to nodeLimit by degree centrality (highest-degree first). */
export function limitEntitiesByDegree(
  allEntities: KGEntity[],
  allRelations: KGRelation[],
  nodeLimit: number,
): KGEntity[] {
  if (allEntities.length <= nodeLimit) return allEntities;
  const deg = computeDegreeMap(allEntities, allRelations);
  return [...allEntities].sort((a, b) => (deg.get(b.id) ?? 0) - (deg.get(a.id) ?? 0)).slice(0, nodeLimit);
}
