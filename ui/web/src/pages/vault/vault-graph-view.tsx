import { useRef, useMemo, useState, useEffect, useLayoutEffect, useCallback } from "react";
import ForceGraph2D, { type ForceGraphMethods } from "react-force-graph-2d";
import { useUiStore } from "@/stores/use-ui-store";
import { buildVaultGraphData, limitVaultDocsByDegree, VAULT_TYPE_COLORS } from "@/adapters/vault-graph-adapter";
import type { VaultGraphNode, VaultGraphLink } from "@/adapters/vault-graph-adapter";
import type { VaultDocument } from "@/types/vault";
import { useVaultGraphData } from "./hooks/use-vault";
import { VaultGraphControls } from "./vault-graph-controls";

const NODE_R = 5;
const DOUBLE_CLICK_MS = 280;
const DEFAULT_NODE_LIMIT = 200;

interface Props {
  agentId: string;
  teamId?: string;
  onNodeClick?: (doc: VaultDocument) => void;
}

export function VaultGraphView({ agentId, teamId, onNodeClick }: Props) {
  const theme = useUiStore((s) => s.theme);
  const isDark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  const { documents: allDocs, links, loading } = useVaultGraphData(agentId, { teamId });

  const graphRef = useRef<ForceGraphMethods<VaultGraphNode, VaultGraphLink>>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dimensions, setDimensions] = useState({ width: 600, height: 400 });
  const [ready, setReady] = useState(false);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [nodeLimit, setNodeLimit] = useState(DEFAULT_NODE_LIMIT);
  const [zoomLevel, setZoomLevel] = useState(1);

  const clickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastClickRef = useRef<string | null>(null);

  // Container sizing
  useLayoutEffect(() => {
    const el = containerRef.current;
    if (el && el.clientWidth > 0 && el.clientHeight > 0) {
      setDimensions({ width: el.clientWidth, height: el.clientHeight });
      setReady(true);
    }
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      if (!entry) return;
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) { setDimensions({ width, height }); setReady(true); }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Limit docs by degree centrality
  const totalCount = allDocs.length;
  const isLimited = totalCount > nodeLimit;
  const documents = useMemo(
    () => limitVaultDocsByDegree(allDocs, links, nodeLimit),
    [allDocs, links, nodeLimit],
  );

  const docMap = useMemo(() => new Map(documents.map((d) => [d.id, d])), [documents]);
  const graphData = useMemo(() => buildVaultGraphData(documents, links), [documents, links]);

  // Highlight sets from selected node
  const { highlightNodes, highlightLinks } = useMemo(() => {
    if (!selectedNodeId) return { highlightNodes: new Set<string>(), highlightLinks: new Set<string>() };
    const node = graphData.nodes.find((n) => n.id === selectedNodeId);
    if (!node) return { highlightNodes: new Set<string>(), highlightLinks: new Set<string>() };
    return { highlightNodes: new Set([selectedNodeId, ...node.neighbors]), highlightLinks: new Set(node.linkIds) };
  }, [selectedNodeId, graphData.nodes]);

  // Node rendering: circle + label + highlight/dim
  const nodeCanvasObject = useCallback(
    (node: VaultGraphNode, ctx: CanvasRenderingContext2D, globalScale: number) => {
      const x = node.x ?? 0, y = node.y ?? 0;
      const isSelected = node.id === selectedNodeId;
      const isNeighbor = highlightNodes.has(node.id);
      const isDimmed = !!selectedNodeId && !isSelected && !isNeighbor;
      const r = NODE_R + Math.min(node.degree, 10);

      ctx.beginPath();
      ctx.arc(x, y, r, 0, 2 * Math.PI);
      ctx.fillStyle = isDimmed ? `${node.color}30` : node.color;
      ctx.fill();

      if (isSelected) {
        ctx.strokeStyle = node.color;
        ctx.lineWidth = 2.5 / globalScale;
        ctx.shadowColor = node.color;
        ctx.shadowBlur = 8 / globalScale;
        ctx.stroke();
        ctx.shadowBlur = 0;
      } else if (isNeighbor) {
        ctx.strokeStyle = node.color;
        ctx.lineWidth = 1.5 / globalScale;
        ctx.stroke();
      }

      if (globalScale > 0.5 || isSelected || isNeighbor) {
        const fontSize = Math.max(11 / globalScale, 2);
        ctx.font = `${isSelected ? "bold " : ""}${fontSize}px Inter, system-ui, sans-serif`;
        ctx.textAlign = "center";
        ctx.textBaseline = "top";
        ctx.fillStyle = isDimmed
          ? (isDark ? "rgba(255,255,255,0.12)" : "rgba(0,0,0,0.12)")
          : (isDark ? "#e2e8f0" : "#1e293b");
        ctx.fillText(node.title.slice(0, 24), x, y + r + 2 / globalScale);
      }
    },
    [selectedNodeId, highlightNodes, isDark],
  );

  // Link label overlay for highlighted links
  const linkCanvasObject = useCallback(
    (link: any, ctx: CanvasRenderingContext2D, globalScale: number) => {
      if (!highlightLinks.has(link.id)) return;
      const src = link.source, tgt = link.target;
      if (!src?.x || !tgt?.x) return;
      const midX = (src.x + tgt.x) / 2, midY = (src.y + tgt.y) / 2;
      const fontSize = Math.max(9 / globalScale, 2);
      const text = link.label || "";
      ctx.font = `${fontSize}px Inter, system-ui, sans-serif`;
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      const metrics = ctx.measureText(text);
      const pad = 2 / globalScale;
      ctx.fillStyle = isDark ? "rgba(15,23,42,0.85)" : "rgba(255,255,255,0.9)";
      ctx.fillRect(midX - metrics.width / 2 - pad, midY - fontSize / 2 - pad, metrics.width + pad * 2, fontSize + pad * 2);
      ctx.fillStyle = isDark ? "#94a3b8" : "#64748b";
      ctx.fillText(text, midX, midY);
    },
    [highlightLinks, isDark],
  );

  // Click: single = highlight, double = open detail
  const handleNodeClick = useCallback(
    (node: VaultGraphNode) => {
      if (clickTimerRef.current && lastClickRef.current === node.id) {
        clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
        lastClickRef.current = null;
        const doc = docMap.get(node.id);
        if (doc && onNodeClick) onNodeClick(doc);
        return;
      }
      if (clickTimerRef.current) clearTimeout(clickTimerRef.current);
      lastClickRef.current = node.id;
      clickTimerRef.current = setTimeout(() => {
        setSelectedNodeId((prev) => (prev === node.id ? null : node.id));
        clickTimerRef.current = null;
      }, DOUBLE_CLICK_MS);
    },
    [docMap, onNodeClick],
  );

  const handleZoomIn = useCallback(() => {
    const fg = graphRef.current;
    if (fg) fg.zoom(Math.min((fg.zoom() ?? 1) * 1.5, 8), 300);
  }, []);
  const handleZoomOut = useCallback(() => {
    const fg = graphRef.current;
    if (fg) fg.zoom(Math.max((fg.zoom() ?? 1) / 1.5, 0.1), 300);
  }, []);
  const handleFitToView = useCallback(() => graphRef.current?.zoomToFit(300, 20), []);

  return (
    <div className="flex flex-col rounded-md border overflow-hidden bg-background" style={{ height: 460 }}>
      {/* Legend */}
      <div className="flex flex-wrap gap-3 text-xs text-muted-foreground px-3 py-1.5 border-b">
        {Object.entries(VAULT_TYPE_COLORS).map(([type, color]) => (
          <span key={type} className="flex items-center gap-1">
            <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />
            {type}
          </span>
        ))}
      </div>

      {/* Graph canvas — containerRef must always mount so ResizeObserver can fire */}
      <div ref={containerRef} className="min-h-0 flex-1 relative">
        {loading && allDocs.length === 0 ? (
          <div className="h-full animate-pulse rounded-md bg-muted" />
        ) : allDocs.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">No documents</div>
        ) : ready ? (
          <ForceGraph2D
            ref={graphRef}
            graphData={graphData}
            width={dimensions.width}
            height={dimensions.height}
            nodeCanvasObject={nodeCanvasObject}
            nodeCanvasObjectMode={() => "replace"}
            nodePointerAreaPaint={(node, color, ctx) => {
              const r = NODE_R + Math.min((node as VaultGraphNode).degree, 10) + 3;
              ctx.beginPath();
              ctx.arc(node.x ?? 0, node.y ?? 0, r, 0, 2 * Math.PI);
              ctx.fillStyle = color;
              ctx.fill();
            }}
            onNodeClick={handleNodeClick}
            onBackgroundClick={() => setSelectedNodeId(null)}
            onZoom={(transform) => setZoomLevel(transform.k)}
            linkColor={(link: any) => highlightLinks.has(link.id) ? "#64748b" : (isDark ? "#334155" : "#cbd5e1")}
            linkWidth={(link: any) => (highlightLinks.has(link.id) ? 2 : 0.5)}
            linkCanvasObject={linkCanvasObject}
            linkCanvasObjectMode={(link: any) => (highlightLinks.has(link.id) ? "after" : undefined)}
            linkDirectionalParticles={(link: any) => (highlightLinks.has(link.id) ? 2 : 0)}
            linkDirectionalParticleWidth={3}
            linkDirectionalParticleSpeed={0.004}
            linkDirectionalArrowLength={4}
            linkDirectionalArrowRelPos={1}
            backgroundColor="transparent"
            d3AlphaDecay={0.04}
            d3VelocityDecay={0.3}
            warmupTicks={40}
            cooldownTime={4000}
            enableNodeDrag
            minZoom={0.1}
            maxZoom={8}
          />
        ) : null}
      </div>

      {/* Stats bar */}
      <VaultGraphControls
        docCount={totalCount}
        linkCount={links.length}
        nodeLimit={nodeLimit}
        isLimited={isLimited}
        zoomLevel={zoomLevel}
        onNodeLimitChange={setNodeLimit}
        onZoomIn={handleZoomIn}
        onZoomOut={handleZoomOut}
        onFitToView={handleFitToView}
      />
    </div>
  );
}
