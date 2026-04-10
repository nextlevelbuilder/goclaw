import { useRef, useEffect, useCallback, useState } from "react";
import Sigma from "sigma";
import { EdgeCurvedArrowProgram } from "@sigma/edge-curve";
import { EdgeArrowProgram } from "sigma/rendering";
import forceAtlas2 from "graphology-layout-forceatlas2";
import noverlap from "graphology-layout-noverlap";
import type Graph from "graphology";
import { useUiStore } from "@/stores/use-ui-store";
import { SIGMA_SETTINGS } from "./graph-utils";

export type EdgeType = "curvedArrow" | "arrow";

export interface SigmaGraphContainerProps {
  graph: Graph;
  edgeType?: EdgeType;
  selectedNodeId?: string | null;
  onNodeSelect?: (nodeId: string | null) => void;
  onNodeDoubleClick?: (nodeId: string) => void;
  /** Called when Sigma instance is ready (or destroyed) */
  onSigmaReady?: (sigma: Sigma | null) => void;
  /** Compact mode for embedded mini-graphs (no layout, smaller labels) */
  compact?: boolean;
  /** Node types to hide (from filter component) */
  hiddenTypes?: Set<string>;
}

/** Theme-aware colors */
function useThemeColors() {
  const theme = useUiStore((s) => s.theme);
  const isDark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  return {
    isDark,
    labelColor: isDark ? "#e2e8f0" : "#1e293b",
    // Base edge color — visible but not dominant
    edgeColor: isDark ? "#47556966" : "#94a3b8cc",
    // Dim color for non-active edges
    dimEdgeColor: isDark ? "#47556933" : "#cbd5e166",
    // Highlight for active neighborhood edges
    highlightEdgeColor: isDark ? "#a1a1aa" : "#64748b",
  };
}

export function SigmaGraphContainer({
  graph,
  edgeType = "arrow",
  selectedNodeId,
  onNodeSelect,
  onNodeDoubleClick,
  onSigmaReady,
  compact = false,
  hiddenTypes,
}: SigmaGraphContainerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const internalSigmaRef = useRef<Sigma | null>(null);
  // Incremented when sigma instance changes — used to trigger event handler registration.
  const [sigmaVersion, setSigmaVersion] = useState(0);
  const [hoveredNode, setHoveredNode] = useState<string | null>(null);
  // Pulse phase for animated highlighted edges (0..1, cycles)
  const [pulsePhase, setPulsePhase] = useState(0);
  const { labelColor, edgeColor, dimEdgeColor, highlightEdgeColor } = useThemeColors();

  const setSigmaRef = useCallback(
    (instance: Sigma | null) => {
      internalSigmaRef.current = instance;
      setSigmaVersion((v) => v + 1);
      onSigmaReady?.(instance);
    },
    [onSigmaReady],
  );

  // --- Initialize Sigma with PRE-COMPUTED FA2 layout (no shake, no worker) ---
  useEffect(() => {
    if (!containerRef.current || graph.order === 0) return;

    // Unified adaptive FA2 layout — no density branching.
    // Params scale with orphan ratio (disconnected nodes / total nodes).
    if (graph.order > 1) {
      // Random disc init — uniform area distribution for all graph types.
      const spread = Math.sqrt(graph.order) * 20;
      const nodes = graph.nodes();
      for (let i = 0; i < nodes.length; i++) {
        const angle = Math.random() * Math.PI * 2;
        const r = Math.sqrt(Math.random()) * spread;
        graph.setNodeAttribute(nodes[i], "x", Math.cos(angle) * r);
        graph.setNodeAttribute(nodes[i], "y", Math.sin(angle) * r);
      }

      // Orphan ratio drives adaptive parameters:
      // High orphan → stronger gravity (pull orphans to center), tighter layout
      // Low orphan  → link forces dominate, more spread for cluster readability
      let orphanCount = 0;
      for (const node of nodes) {
        if (graph.degree(node) === 0) orphanCount++;
      }
      const orphanRatio = orphanCount / graph.order;
      const gravity = 0.5 + orphanRatio * 2.0;
      const scalingRatio = 4.0 - orphanRatio * 2.0;
      const iterations = graph.order < 100 ? 500 : graph.order < 500 ? 300 : 200;

      forceAtlas2.assign(graph, {
        iterations,
        settings: {
          linLogMode: true,
          outboundAttractionDistribution: true,
          gravity,
          scalingRatio,
          adjustSizes: true,
          strongGravityMode: true,
          slowDown: 8,
          barnesHutOptimize: graph.order > 100,
          edgeWeightInfluence: 0,
        },
      });

      // Noverlap only for connected-heavy graphs (prevents overlap in dense clusters).
      // Skip for high-orphan graphs to avoid grid artifacts.
      if (orphanRatio < 0.3) {
        noverlap.assign(graph, {
          maxIterations: 30,
          settings: { margin: 2, ratio: 1.02, speed: 3, gridSize: 20 },
        });
      }
    }

    const edgePrograms: Record<string, typeof EdgeArrowProgram> = {
      arrow: EdgeArrowProgram,
      curvedArrow: EdgeCurvedArrowProgram as unknown as typeof EdgeArrowProgram,
    };

    const sigma = new Sigma(graph, containerRef.current, {
      allowInvalidContainer: true,
      renderLabels: true,
      labelRenderedSizeThreshold: compact ? 14 : SIGMA_SETTINGS.labelRenderedSizeThreshold,
      labelDensity: compact ? 0.05 : SIGMA_SETTINGS.labelDensity,
      labelGridCellSize: SIGMA_SETTINGS.labelGridCellSize,
      labelColor: { color: labelColor },
      defaultEdgeColor: edgeColor,
      defaultEdgeType: edgeType,
      edgeProgramClasses: edgePrograms,
      minCameraRatio: SIGMA_SETTINGS.minCameraRatio,
      maxCameraRatio: SIGMA_SETTINGS.maxCameraRatio,
      labelFont: "Inter, system-ui, sans-serif",
      labelSize: compact ? 10 : 12,
      zoomingRatio: 1.3,
      // Enable z-index sorting — required for edge/node `zIndex` attr to affect render order
      zIndex: true,
    });

    setSigmaRef(sigma);

    // Fit viewport to graph on next frame (after initial render)
    requestAnimationFrame(() => {
      sigma.getCamera().animatedReset({ duration: 300 });
    });

    return () => {
      sigma.kill();
      // Only clear external ref if it still points to THIS sigma (concurrent mode safety)
      if (internalSigmaRef.current === sigma) {
        internalSigmaRef.current = null;
        onSigmaReady?.(null);
      }
    };
  }, [graph, edgeType, compact]); // eslint-disable-line react-hooks/exhaustive-deps

  // --- Update theme colors without re-init ---
  useEffect(() => {
    const sigma = internalSigmaRef.current;
    if (!sigma) return;
    sigma.setSetting("labelColor", { color: labelColor });
    sigma.setSetting("defaultEdgeColor", edgeColor);
    sigma.refresh();
  }, [labelColor, edgeColor]);

  // Compute multi-hop neighborhood (BFS, 2 hops) for active node
  const neighborhoodRef = useRef<{ nodes: Set<string>; edges: Set<string> } | null>(null);
  useEffect(() => {
    const active = selectedNodeId || hoveredNode;
    if (!active || !graph.hasNode(active)) {
      neighborhoodRef.current = null;
      return;
    }
    const nodes = new Set<string>([active]);
    const edges = new Set<string>();
    const MAX_HOPS = 2;
    let frontier: string[] = [active];
    for (let hop = 0; hop < MAX_HOPS; hop++) {
      const next: string[] = [];
      for (const n of frontier) {
        graph.forEachEdge(n, (edge, _attrs, source, target) => {
          edges.add(edge);
          const other = source === n ? target : source;
          if (!nodes.has(other)) {
            nodes.add(other);
            next.push(other);
          }
        });
      }
      frontier = next;
      if (frontier.length === 0) break;
    }
    neighborhoodRef.current = { nodes, edges };
  }, [selectedNodeId, hoveredNode, graph]);

  // --- Unified node/edge reducers: filter + subtle hover highlight (no dimming) ---
  useEffect(() => {
    const sigma = internalSigmaRef.current;
    if (!sigma) return;

    const getNodeType = (attrs: Record<string, unknown>) =>
      (attrs.docType || attrs.entityType || "other") as string;

    sigma.setSetting("nodeReducer", (node, data) => {
      // Filter: hide nodes of hidden types
      if (hiddenTypes?.size && hiddenTypes.has(getNodeType(data))) {
        return { ...data, hidden: true };
      }

      const activeNode = selectedNodeId || hoveredNode;
      if (!activeNode) return data;

      const hood = neighborhoodRef.current;
      if (!hood) return data;

      if (node === activeNode) {
        // Active node: bring to top, show label
        return { ...data, zIndex: 3, forceLabel: true };
      }
      if (hood.nodes.has(node)) {
        // Within 2-hop neighborhood: show label, slightly raised z-index
        return { ...data, zIndex: 2, forceLabel: true };
      }
      // Outside neighborhood: keep color/size, no dimming — just lower z-index
      return { ...data, zIndex: 0 };
    });

    sigma.setSetting("edgeReducer", (edge, data) => {
      // Filter: hide edges connected to hidden node types
      if (hiddenTypes?.size) {
        const srcAttrs = graph.getNodeAttributes(graph.source(edge));
        const tgtAttrs = graph.getNodeAttributes(graph.target(edge));
        if (hiddenTypes.has(getNodeType(srcAttrs)) || hiddenTypes.has(getNodeType(tgtAttrs))) {
          return { ...data, hidden: true };
        }
      }

      const activeNode = selectedNodeId || hoveredNode;
      if (!activeNode) return data;

      const hood = neighborhoodRef.current;
      if (!hood) return data;

      if (hood.edges.has(edge)) {
        // Edges within neighborhood: thin + subtle OPACITY pulse (not size).
        const alpha = Math.round(200 + Math.sin(pulsePhase * Math.PI * 2) * 40);
        const alphaHex = Math.max(0, Math.min(255, alpha)).toString(16).padStart(2, "0");
        const pulsedColor = `${highlightEdgeColor}${alphaHex}`;
        return { ...data, color: pulsedColor, size: 1, zIndex: 2 };
      }
      // Non-related edges: HIDE entirely so active edges are clearly visible
      return { ...data, hidden: true };
    });

    sigma.refresh();
  }, [selectedNodeId, hoveredNode, graph, highlightEdgeColor, dimEdgeColor, hiddenTypes]);

  // --- Pulse animation for highlighted edges (only runs when a node is active) ---
  // Respects prefers-reduced-motion — skips animation entirely for accessibility
  useEffect(() => {
    const active = selectedNodeId || hoveredNode;
    if (!active) return;

    // Honor user's reduced-motion preference
    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    if (mediaQuery.matches) return;

    let rafId = 0;
    const start = performance.now();
    const PULSE_PERIOD_MS = 1800; // slower, gentler pulse
    const tick = () => {
      const elapsed = performance.now() - start;
      setPulsePhase((elapsed % PULSE_PERIOD_MS) / PULSE_PERIOD_MS);
      rafId = requestAnimationFrame(tick);
    };
    rafId = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafId);
  }, [selectedNodeId, hoveredNode]);

  // Add pulsePhase to reducer deps so edges re-render on pulse
  useEffect(() => {
    const sigma = internalSigmaRef.current;
    if (!sigma) return;
    sigma.refresh({ skipIndexation: true });
  }, [pulsePhase]);

  // --- Event handlers: use refs for values that change frequently to avoid
  // re-registering Sigma listeners (which drops in-flight double-click events) ---
  const selectedNodeIdRef = useRef(selectedNodeId);
  selectedNodeIdRef.current = selectedNodeId;
  const onNodeSelectRef = useRef(onNodeSelect);
  onNodeSelectRef.current = onNodeSelect;
  const onNodeDoubleClickRef = useRef(onNodeDoubleClick);
  onNodeDoubleClickRef.current = onNodeDoubleClick;

  useEffect(() => {
    const sigma = internalSigmaRef.current;
    if (!sigma) return;

    const handleEnterNode = ({ node }: { node: string }) => {
      setHoveredNode(node);
      if (containerRef.current) containerRef.current.style.cursor = "pointer";
    };

    const handleLeaveNode = () => {
      setHoveredNode(null);
      if (containerRef.current) containerRef.current.style.cursor = "default";
    };

    const handleClickNode = ({ node }: { node: string }) => {
      onNodeSelectRef.current?.(node === selectedNodeIdRef.current ? null : node);
    };

    const handleDoubleClickNode = ({ node, event }: { node: string; event: { preventSigmaDefault?: () => void } }) => {
      // Prevent Sigma's default zoom-in behavior on double-click
      event.preventSigmaDefault?.();
      onNodeDoubleClickRef.current?.(node);
    };

    const handleClickStage = () => {
      onNodeSelectRef.current?.(null);
    };

    sigma.on("enterNode", handleEnterNode);
    sigma.on("leaveNode", handleLeaveNode);
    sigma.on("clickNode", handleClickNode);
    sigma.on("doubleClickNode", handleDoubleClickNode);
    sigma.on("clickStage", handleClickStage);

    return () => {
      sigma.off("enterNode", handleEnterNode);
      sigma.off("leaveNode", handleLeaveNode);
      sigma.off("clickNode", handleClickNode);
      sigma.off("doubleClickNode", handleDoubleClickNode);
      sigma.off("clickStage", handleClickStage);
    };
  }, [sigmaVersion]); // re-register only when sigma instance changes

  // NOTE: Click on node NO LONGER moves camera.
  // Camera only animates for explicit user actions (search, fit-to-view, keyboard F).
  // This matches the old force-graph behavior where clicking just highlights.

  // No-data state
  if (graph.order === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        No data to display
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="h-full w-full"
      style={{ minHeight: compact ? 200 : 300 }}
    />
  );
}
