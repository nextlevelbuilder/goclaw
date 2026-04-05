import { useMemo, useEffect, useCallback, useState, useTransition, memo } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useEdgesState,
  useReactFlow,
  Background,
  Controls,
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

const GRAPH_LIMIT = 50;

// Dual-theme palette — separate dark/light values for readability on both backgrounds
interface TypeColor {
  border: string;
  dark: { bg: string; text: string };
  light: { bg: string; text: string };
}
const TYPE_COLORS: Record<string, TypeColor> = {
  person:       { border: "#E85D24", dark: { bg: "rgba(232,93,36,0.15)", text: "#f4a574" },  light: { bg: "#fde8d8", text: "#7a2610" } },
  organization: { border: "#ef4444", dark: { bg: "rgba(239,68,68,0.15)", text: "#fca5a5" },  light: { bg: "#fee2e2", text: "#991b1b" } },
  project:      { border: "#22c55e", dark: { bg: "rgba(34,197,94,0.15)", text: "#86efac" },  light: { bg: "#dcfce7", text: "#166534" } },
  product:      { border: "#f97316", dark: { bg: "rgba(249,115,22,0.15)", text: "#fdba74" }, light: { bg: "#ffedd5", text: "#9a3412" } },
  technology:   { border: "#3b82f6", dark: { bg: "rgba(59,130,246,0.15)", text: "#93c5fd" }, light: { bg: "#dbeafe", text: "#1e40af" } },
  task:         { border: "#f59e0b", dark: { bg: "rgba(245,158,11,0.15)", text: "#fcd34d" }, light: { bg: "#fef3c7", text: "#92400e" } },
  event:        { border: "#ec4899", dark: { bg: "rgba(236,72,153,0.15)", text: "#f9a8d4" }, light: { bg: "#fce7f3", text: "#9d174d" } },
  document:     { border: "#8b5cf6", dark: { bg: "rgba(139,92,246,0.15)", text: "#c4b5fd" }, light: { bg: "#ede9fe", text: "#6d28d9" } },
  concept:      { border: "#a78bfa", dark: { bg: "rgba(167,139,250,0.12)", text: "#c4b5fd" }, light: { bg: "#ede9fe", text: "#5b21b6" } },
  location:     { border: "#14b8a6", dark: { bg: "rgba(20,184,166,0.15)", text: "#5eead4" },  light: { bg: "#ccfbf1", text: "#115e59" } },
};
const DEFAULT_TC: TypeColor = { border: "#9ca3af", dark: { bg: "rgba(156,163,175,0.12)", text: "#d1d5db" }, light: { bg: "#f3f4f6", text: "#374151" } };

const TYPE_MASS: Record<string, number> = {
  organization: 8, project: 6, product: 5, person: 4, technology: 3.5, concept: 3, location: 3, document: 2.5, event: 2, task: 1.5,
};
const DEFAULT_MASS = 2;

function computeDegreeMap(entities: KGEntity[], relations: KGRelation[]): Map<string, number> {
  const deg = new Map<string, number>();
  const ids = new Set(entities.map((e) => e.id));
  for (const r of relations) {
    if (ids.has(r.source_entity_id)) deg.set(r.source_entity_id, (deg.get(r.source_entity_id) ?? 0) + 1);
    if (ids.has(r.target_entity_id)) deg.set(r.target_entity_id, (deg.get(r.target_entity_id) ?? 0) + 1);
  }
  return deg;
}

const HANDLE_STYLE = { opacity: 0, width: 0, height: 0, pointerEvents: "none" as const };

// memo prevents re-render during pan/zoom — only re-renders when data changes
const EntityNode = memo(function EntityNode({ data }: { data: { label: string; type: string; degree: number; isDark: boolean } }) {
  const tc = TYPE_COLORS[data.type] || DEFAULT_TC;
  const t = data.isDark ? tc.dark : tc.light;
  const isHub = data.degree >= 4;
  return (
    <div
      className="px-4 py-1.5 cursor-grab"
      style={{
        background: t.bg,
        border: `2px solid ${tc.border}`,
        borderRadius: 20,
        boxShadow: isHub ? `0 0 8px ${tc.border}40` : undefined,
      }}
    >
      <Handle type="target" position={Position.Top} style={HANDLE_STYLE} />
      <Handle type="source" position={Position.Bottom} style={HANDLE_STYLE} />
      <div className="text-[11px] font-semibold whitespace-nowrap" style={{ color: t.text }}>
        {data.label}
      </div>
      <div className="text-[9px] text-center" style={{ color: t.text, opacity: 0.6 }}>
        {data.type}
      </div>
    </div>
  );
});

const nodeTypes = { entity: EntityNode };

interface SimNode extends SimulationNodeDatum { id: string; mass: number }

function buildGraph(entities: KGEntity[], relations: KGRelation[], isDark: boolean) {
  const entityIds = new Set(entities.map((e) => e.id));
  const degreeMap = computeDegreeMap(entities, relations);
  const nodes: Node[] = entities.map((e) => ({
    id: e.id, type: "entity", position: { x: 0, y: 0 },
    data: { label: e.name, type: e.entity_type, degree: degreeMap.get(e.id) ?? 0, isDark },
  }));
  // Edge color handled by ReactFlow colorMode — use neutral gray
  const edgeColor = "#64748b";
  const edges: Edge[] = relations
    .filter((r) => entityIds.has(r.source_entity_id) && entityIds.has(r.target_entity_id))
    .map((r) => ({
      id: r.id, source: r.source_entity_id, target: r.target_entity_id,
      label: undefined, data: { relationLabel: r.relation_type.replace(/_/g, " ") },
      animated: false, type: "default",
      style: { stroke: edgeColor, strokeWidth: 2, opacity: 0.6 },
    }));
  return { nodes, edges };
}

function computeForceLayout(nodes: Node[], edges: Edge[], entities: KGEntity[]): Node[] {
  if (nodes.length === 0) return nodes;
  const entityTypeMap = new Map(entities.map((e) => [e.id, e.entity_type]));
  const simNodes: SimNode[] = nodes.map((n) => ({
    id: n.id, x: n.position.x, y: n.position.y,
    mass: TYPE_MASS[entityTypeMap.get(n.id) ?? ""] ?? DEFAULT_MASS,
  }));
  const simLinks = edges.map((e) => ({ source: e.source, target: e.target }));
  const w = 600, h = 400;
  // Scale forces by node count — tighter for small graphs, spread for large
  const n = nodes.length;
  const linkDist = n > 60 ? 200 : n > 30 ? 160 : 120;
  const chargeMul = n > 60 ? -250 : n > 30 ? -180 : -120;
  const centerPull = n > 60 ? 0.03 : n > 30 ? 0.05 : 0.08;
  const collideBase = n > 60 ? 50 : n > 30 ? 40 : 35;
  const simulation = forceSimulation(simNodes)
    .force("link", forceLink(simLinks).id((d: any) => d.id).distance(linkDist).strength(0.5))
    .force("charge", forceManyBody().strength((d: any) => chargeMul * (d.mass ?? 1)))
    .force("center", forceCenter(w / 2, h / 2))
    .force("x", forceX(w / 2).strength(centerPull))
    .force("y", forceY(h / 2).strength(centerPull))
    .force("collide", forceCollide().radius((d: any) => collideBase + (d.mass ?? 1) * 3).strength(0.7))
    .stop();
  const ticks = Math.ceil(Math.log(simulation.alphaMin()) / Math.log(1 - simulation.alphaDecay()));
  for (let i = 0; i < ticks; i++) simulation.tick();
  return nodes.map((n, i) => ({ ...n, position: { x: simNodes[i]!.x ?? 0, y: simNodes[i]!.y ?? 0 } }));
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

function KGGraphViewInner({ entities: allEntities, relations: allRelations, onEntityClick }: KGGraphViewProps) {
  const { t } = useTranslation("memory");
  const { fitView } = useReactFlow();
  const theme = useUiStore((s) => s.theme);
  const colorMode: ColorMode = theme === "system" ? "system" : theme;
  const isDark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [layoutReady, setLayoutReady] = useState(false);

  // Limit entities to GRAPH_LIMIT, filter relations accordingly
  const totalCount = allEntities.length;
  const isLimited = totalCount > GRAPH_LIMIT;
  // Rank by degree centrality — hub nodes (most connections) always included in graph
  const entities = useMemo(() => {
    if (!isLimited) return allEntities;
    const deg = computeDegreeMap(allEntities, allRelations);
    return [...allEntities].sort((a, b) => (deg.get(b.id) ?? 0) - (deg.get(a.id) ?? 0)).slice(0, GRAPH_LIMIT);
  }, [allEntities, allRelations, isLimited]);
  const entityIds = useMemo(() => new Set(entities.map((e) => e.id)), [entities]);
  const relations = useMemo(
    () => allRelations.filter((r) => entityIds.has(r.source_entity_id) && entityIds.has(r.target_entity_id)),
    [allRelations, entityIds],
  );

  // Build graph — isDark only affects node data colors, not layout
  const { rawNodes, rawEdges } = useMemo(() => {
    const { nodes, edges } = buildGraph(entities, relations, isDark);
    return { rawNodes: nodes, rawEdges: edges };
  }, [entities, relations, isDark]);

  const [layoutNodes, setLayoutNodes] = useState<Node[]>([]);
  const [nodes, setNodes, onNodesChange] = useNodesState(layoutNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(rawEdges);

  // Track which entity set was laid out — only re-layout when data changes, not theme
  const [layoutKey, setLayoutKey] = useState("");
  const dataKey = useMemo(() => entities.map((e) => e.id).join(","), [entities]);

  useEffect(() => {
    if (dataKey === layoutKey) {
      // Theme change only — update node colors without re-layout
      const freshMap = new Map(rawNodes.map((r) => [r.id, r.data]));
      setNodes((prev) => prev.map((n) => {
        const data = freshMap.get(n.id);
        return data ? { ...n, data } : n;
      }));
      return;
    }
    // Data changed — full re-layout
    setLayoutReady(false);
    const timer = setTimeout(() => {
      const positioned = computeForceLayout(rawNodes, rawEdges, entities);
      setLayoutNodes(positioned);
      setNodes(positioned);
      setLayoutKey(dataKey);
      setLayoutReady(true);
      requestAnimationFrame(() => fitView({ padding: 0.15, duration: 300 }));
    }, 0);
    return () => clearTimeout(timer);
  }, [rawNodes, rawEdges, entities, dataKey, layoutKey, setNodes, fitView]);

  useEffect(() => { setEdges(rawEdges); }, [rawEdges, setEdges]);

  // Selection: highlight connected edges, dim others
  const entityMap = useMemo(() => new Map(entities.map((e) => [e.id, e])), [entities]);

  useEffect(() => {
    if (!selectedNodeId) {
      setEdges((eds) => eds.map((e) => ({
        ...e, label: undefined, animated: false,
        style: { stroke: "#64748b", strokeWidth: 2, opacity: 0.6 },
        labelStyle: undefined, labelBgStyle: undefined, labelBgPadding: undefined, labelShowBg: undefined,
      })));
      return;
    }
    const ent = entityMap.get(selectedNodeId);
    const tc = TYPE_COLORS[ent?.entity_type ?? ""] || DEFAULT_TC;
    const labelColor = isDark ? tc.dark.text : tc.light.text;
    const fadedEdge = isDark ? "#1e293b" : "#e2e8f0";
    const labelBg = isDark ? "rgba(15,23,42,0.85)" : "rgba(255,255,255,0.9)";
    setEdges((eds) => eds.map((e) => {
      const connected = e.source === selectedNodeId || e.target === selectedNodeId;
      if (!connected) return { ...e, label: undefined, animated: false, style: { stroke: fadedEdge, strokeWidth: 0.5, opacity: 0.25 } };
      return {
        ...e,
        label: (e.data as { relationLabel?: string })?.relationLabel,
        animated: true,
        style: { stroke: tc.border, strokeWidth: 3, opacity: 0.9 },
        labelStyle: { fontSize: 9, fill: labelColor, fontWeight: 500 },
        labelBgStyle: { fill: labelBg, stroke: tc.border, rx: 4 },
        labelBgPadding: [4, 2] as [number, number],
        labelShowBg: true,
      };
    }));
  }, [selectedNodeId, entityMap, isDark, setEdges]);

  const [, startTransition] = useTransition();

  const handleNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    startTransition(() => setSelectedNodeId((prev) => (prev === node.id ? null : node.id)));
    // Defer dialog open to next frame — decouples edge restyle from dialog mount cost
    const entity = entityMap.get(node.id);
    if (entity && onEntityClick) setTimeout(() => onEntityClick(entity), 0);
  }, [entityMap, onEntityClick, startTransition]);

  const handlePaneClick = useCallback(() => {
    startTransition(() => setSelectedNodeId(null));
  }, [startTransition]);

  if (allEntities.length === 0) {
    return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{t("kg.graphView.empty")}</div>;
  }

  if (!layoutReady) {
    return <div className="flex h-full items-center justify-center bg-background"><div className="text-sm text-muted-foreground">{t("kg.graphView.loading")}</div></div>;
  }

  return (
    <div className="flex h-full flex-col rounded-md border overflow-hidden bg-background">
      <div className="min-h-0 flex-1">
        <ReactFlow
          nodes={nodes} edges={edges}
          onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
          onNodeClick={handleNodeClick} onPaneClick={handlePaneClick}
          nodeTypes={nodeTypes} colorMode={colorMode}
          minZoom={0.1} maxZoom={3}
          nodesConnectable={false} nodesDraggable={true} elementsSelectable={true}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={20} size={1} />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>

      {/* Stats bar */}
      <div className="flex items-center gap-3 px-3 py-1.5 border-t text-[10px] text-muted-foreground">
        <span>{t("kg.graphView.nodes", { count: totalCount })}</span>
        <span>{t("kg.graphView.edges", { count: allRelations.length })}</span>
        {isLimited && (
          <span title={t("kg.graphView.limitHint", { limit: GRAPH_LIMIT, total: totalCount })}>
            · {t("kg.graphView.limitNote", { limit: GRAPH_LIMIT, total: totalCount })}
          </span>
        )}
      </div>
    </div>
  );
}
