import { useMemo, useEffect, useCallback, useRef } from "react";
import {
  ReactFlow,
  useNodesState,
  useEdgesState,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  Handle,
  Position,
} from "@xyflow/react";
import { forceSimulation, forceLink, forceManyBody, forceCenter, forceCollide, forceX, forceY, type SimulationNodeDatum } from "d3-force";
import "@xyflow/react/dist/style.css";
import type { KGEntity, KGRelation } from "@/types/knowledge-graph";

// Color mapping for entity types
const TYPE_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  person:       { bg: "#dbeafe", border: "#3b82f6", text: "#1e40af" },
  project:      { bg: "#dcfce7", border: "#22c55e", text: "#166534" },
  task:         { bg: "#fef3c7", border: "#f59e0b", text: "#92400e" },
  event:        { bg: "#fce7f3", border: "#ec4899", text: "#9d174d" },
  concept:      { bg: "#e0e7ff", border: "#6366f1", text: "#3730a3" },
  location:     { bg: "#ccfbf1", border: "#14b8a6", text: "#115e59" },
  organization: { bg: "#fee2e2", border: "#ef4444", text: "#991b1b" },
};

const DEFAULT_COLOR = { bg: "#f3f4f6", border: "#9ca3af", text: "#374151" };

function EntityNode({ data }: { data: { label: string; type: string; description?: string } }) {
  const colors = TYPE_COLORS[data.type] || DEFAULT_COLOR;
  return (
    <>
      <Handle type="target" position={Position.Top} className="!bg-transparent !border-0 !w-3 !h-3" />
      <div
        className="px-3 py-2 rounded-lg shadow-sm border-2 min-w-[80px] max-w-[180px] cursor-grab"
        style={{ background: colors.bg, borderColor: colors.border }}
      >
        <div className="text-xs font-semibold truncate" style={{ color: colors.text }}>
          {data.label}
        </div>
        <div className="text-[10px] opacity-60" style={{ color: colors.text }}>
          {data.type}
        </div>
      </div>
      <Handle type="source" position={Position.Bottom} className="!bg-transparent !border-0 !w-3 !h-3" />
    </>
  );
}

const nodeTypes = { entity: EntityNode };

interface SimNode extends SimulationNodeDatum {
  id: string;
}

function buildGraph(entities: KGEntity[], relations: KGRelation[]) {
  const entityIds = new Set(entities.map((e) => e.id));

  const nodes: Node[] = entities.map((e) => ({
    id: e.id,
    type: "entity",
    position: { x: 0, y: 0 },
    data: { label: e.name, type: e.entity_type, description: e.description },
  }));

  const edges: Edge[] = relations
    .filter((r) => entityIds.has(r.source_entity_id) && entityIds.has(r.target_entity_id))
    .map((r) => ({
      id: r.id,
      source: r.source_entity_id,
      target: r.target_entity_id,
      label: r.relation_type.replace(/_/g, " "),
      animated: false,
      style: { stroke: "#94a3b8", strokeWidth: 1.5 },
      labelStyle: { fontSize: 10, fill: "#64748b" },
      labelBgStyle: { fill: "#f8fafc", stroke: "#e2e8f0" },
      labelBgPadding: [4, 2] as [number, number],
      labelShowBg: true,
    }));

  return { nodes, edges };
}

function applyForceLayout(
  nodes: Node[],
  edges: Edge[],
  onUpdate: (positioned: Node[]) => void,
) {
  if (nodes.length === 0) return () => {};

  const simNodes: SimNode[] = nodes.map((n) => ({ id: n.id, x: n.position.x, y: n.position.y }));
  const simLinks = edges.map((e) => ({ source: e.source, target: e.target }));

  const w = 600;
  const h = 400;

  const simulation = forceSimulation(simNodes)
    .force("link", forceLink(simLinks).id((d: any) => d.id).distance(140))
    .force("charge", forceManyBody().strength(-350))
    .force("center", forceCenter(w / 2, h / 2))
    .force("x", forceX(w / 2).strength(0.05))
    .force("y", forceY(h / 2).strength(0.05))
    .force("collide", forceCollide(55));

  simulation.on("tick", () => {
    const positioned = nodes.map((n, i) => ({
      ...n,
      position: { x: simNodes[i]!.x ?? 0, y: simNodes[i]!.y ?? 0 },
    }));
    onUpdate(positioned);
  });

  // Run fast to settle
  simulation.alpha(1).restart();

  return () => simulation.stop();
}

interface KGGraphViewProps {
  entities: KGEntity[];
  relations: KGRelation[];
  onEntityClick?: (entity: KGEntity) => void;
}

export function KGGraphView({ entities, relations, onEntityClick }: KGGraphViewProps) {
  const { nodes: initialNodes, edges: initialEdges } = useMemo(
    () => buildGraph(entities, relations),
    [entities, relations],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);
  const simRef = useRef<(() => void) | null>(null);

  // Run force layout when data changes
  useEffect(() => {
    setEdges(initialEdges);
    simRef.current?.();
    simRef.current = applyForceLayout(initialNodes, initialEdges, setNodes);
    return () => simRef.current?.();
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      if (!onEntityClick) return;
      const entity = entities.find((e) => e.id === node.id);
      if (entity) onEntityClick(entity);
    },
    [entities, onEntityClick],
  );

  if (entities.length === 0) {
    return (
      <div className="flex items-center justify-center h-[400px] text-sm text-muted-foreground">
        No entities to visualize
      </div>
    );
  }

  return (
    <div className="h-[500px] rounded-md border bg-background">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.2}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeColor={(n) => {
            const type = (n.data as any)?.type as string;
            return (TYPE_COLORS[type] || DEFAULT_COLOR).border;
          }}
          maskColor="rgba(0,0,0,0.1)"
        />
      </ReactFlow>

      {/* Legend */}
      <div className="flex flex-wrap gap-2 px-3 py-2 border-t text-[10px]">
        {Object.entries(TYPE_COLORS).map(([type, colors]) => (
          <div key={type} className="flex items-center gap-1">
            <div className="w-2.5 h-2.5 rounded-full" style={{ background: colors.border }} />
            <span className="text-muted-foreground">{type}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
