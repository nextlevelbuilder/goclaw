import { useMemo, useEffect, useCallback } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useEdgesState,
  useReactFlow,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  type ColorMode,
  Handle,
  Position,
} from "@xyflow/react";
import { forceSimulation, forceLink, forceManyBody, forceCenter, forceCollide, forceX, forceY, type SimulationNodeDatum } from "d3-force";
import "@xyflow/react/dist/style.css";
import { useTranslation } from "react-i18next";
import { useUiStore } from "@/stores/use-ui-store";
import type { KGEntity, KGRelation } from "@/types/knowledge-graph";

// Color mapping for entity types
const TYPE_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  person:       { bg: "#fde8d8", border: "#E85D24", text: "#7a2610" },
  project:      { bg: "#dcfce7", border: "#22c55e", text: "#166534" },
  task:         { bg: "#fef3c7", border: "#f59e0b", text: "#92400e" },
  event:        { bg: "#fce7f3", border: "#ec4899", text: "#9d174d" },
  concept:      { bg: "#fef5e0", border: "#F8D080", text: "#7a5010" },
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

/** Run d3-force simulation synchronously and return final positions (no per-tick renders). */
function computeForceLayout(nodes: Node[], edges: Edge[]): Node[] {
  if (nodes.length === 0) return nodes;

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
    .force("collide", forceCollide(55))
    .stop();

  // Run simulation to completion synchronously (~300 ticks → 1 render instead of 300)
  const ticks = Math.ceil(Math.log(simulation.alphaMin()) / Math.log(1 - simulation.alphaDecay()));
  for (let i = 0; i < ticks; i++) simulation.tick();

  return nodes.map((n, i) => ({
    ...n,
    position: { x: simNodes[i]!.x ?? 0, y: simNodes[i]!.y ?? 0 },
  }));
}

interface KGGraphViewProps {
  entities: KGEntity[];
  relations: KGRelation[];
  onEntityClick?: (entity: KGEntity) => void;
}

export function KGGraphView(props: KGGraphViewProps) {
  return (
    <ReactFlowProvider>
      <KGGraphViewInner {...props} />
    </ReactFlowProvider>
  );
}

function KGGraphViewInner({ entities, relations, onEntityClick }: KGGraphViewProps) {
  const { t } = useTranslation("memory");
  const { fitView } = useReactFlow();
  const theme = useUiStore((s) => s.theme);
  const colorMode: ColorMode = theme === "system" ? "system" : theme;
  // Compute layout synchronously — no per-tick re-renders
  const { layoutNodes, layoutEdges } = useMemo(() => {
    const { nodes: rawNodes, edges: rawEdges } = buildGraph(entities, relations);
    return { layoutNodes: computeForceLayout(rawNodes, rawEdges), layoutEdges: rawEdges };
  }, [entities, relations]);

  const [nodes, setNodes, onNodesChange] = useNodesState(layoutNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(layoutEdges);

  // Sync when data changes
  useEffect(() => {
    setNodes(layoutNodes);
    setEdges(layoutEdges);
    requestAnimationFrame(() => fitView({ padding: 0.15, duration: 300 }));
  }, [layoutNodes, layoutEdges, setNodes, setEdges, fitView]);

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
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {t("kg.graphView.empty")}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col rounded-md border bg-background">
      <div className="min-h-0 flex-1">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={handleNodeClick}
          nodeTypes={nodeTypes}
          colorMode={colorMode}
          fitView
          minZoom={0.1}
          maxZoom={3}
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
            style={{ width: 100, height: 75 }}
          />
        </ReactFlow>
      </div>

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
