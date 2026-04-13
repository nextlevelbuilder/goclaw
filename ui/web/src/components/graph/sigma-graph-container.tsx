import { useRef, useEffect, useCallback, useState } from "react";
import Sigma from "sigma";
import { EdgeCurvedArrowProgram } from "@sigma/edge-curve";
import { EdgeArrowProgram } from "sigma/rendering";
import FA2Layout from "graphology-layout-forceatlas2/worker";
import forceAtlas2 from "graphology-layout-forceatlas2";
import noverlap from "graphology-layout-noverlap";
import type Graph from "graphology";
import { useUiStore } from "@/stores/use-ui-store";
import { SIGMA_SETTINGS, getFA2WorkerSettings, ZOOM_TIERS, TIER_MIN_DEGREE, TIER_EDGE_DEGREE } from "./graph-utils";
import { getVaultNodeColor } from "@/adapters/vault-graph-adapter";

export type EdgeType = "curvedArrow" | "arrow";

export interface SigmaGraphContainerProps {
  graph: Graph;
  edgeType?: EdgeType;
  selectedNodeId?: string | null;
  onNodeSelect?: (nodeId: string | null) => void;
  onNodeDoubleClick?: (nodeId: string) => void;
  /** Called when Sigma instance is ready (or destroyed) */
  onSigmaReady?: (sigma: Sigma | null) => void;
  /** Called when FA2 layout starts/stops */
  onLayoutStateChange?: (running: boolean) => void;
  /** Compact mode for embedded mini-graphs (no layout, smaller labels) */
  compact?: boolean;
  /** Node types to hide (from filter component) */
  hiddenTypes?: Set<string>;
}

/** Theme-aware colors — derived from store value, not DOM class.
 *  DOM class lags behind store update, causing stale reads on theme toggle. */
function useThemeColors() {
  const theme = useUiStore((s) => s.theme);
  const systemDark = typeof window !== "undefined"
    && window.matchMedia("(prefers-color-scheme: dark)").matches;
  const isDark = theme === "dark" || (theme === "system" && systemDark);
  return {
    isDark,
    labelColor: isDark ? "#e2e8f0" : "#1e293b",
    edgeColor: isDark ? "#71717a" : "#d4d4d8",
    highlightEdgeColor: isDark ? "#d4d4d8" : "#3f3f46",
  };
}

/** Reposition orphan nodes (degree=0) into a compact ring around the connected cluster.
 *  Prevents FA2 from scattering them far away and forcing camera to zoom out. */
function compactOrphans(graph: Graph) {
  const connected: { x: number; y: number }[] = [];
  const orphans: string[] = [];

  graph.forEachNode((node) => {
    if (graph.degree(node) === 0) {
      orphans.push(node);
    } else {
      connected.push({
        x: graph.getNodeAttribute(node, "x") as number,
        y: graph.getNodeAttribute(node, "y") as number,
      });
    }
  });

  if (orphans.length === 0 || connected.length === 0) return;

  // Compute bounding circle of connected nodes
  let cx = 0, cy = 0;
  for (const p of connected) { cx += p.x; cy += p.y; }
  cx /= connected.length;
  cy /= connected.length;

  let maxR = 0;
  for (const p of connected) {
    const d = Math.sqrt((p.x - cx) ** 2 + (p.y - cy) ** 2);
    if (d > maxR) maxR = d;
  }

  // Place orphans in a ring just outside the cluster (1.1-1.4x radius)
  const innerR = maxR * 1.1;
  const outerR = maxR * 1.4;
  for (let i = 0; i < orphans.length; i++) {
    const angle = (i / orphans.length) * Math.PI * 2 + Math.random() * 0.3;
    const r = innerR + Math.random() * (outerR - innerR);
    graph.setNodeAttribute(orphans[i], "x", cx + Math.cos(angle) * r);
    graph.setNodeAttribute(orphans[i], "y", cy + Math.sin(angle) * r);
  }
}

export function SigmaGraphContainer({
  graph,
  edgeType = "arrow",
  selectedNodeId,
  onNodeSelect,
  onNodeDoubleClick,
  onSigmaReady,
  onLayoutStateChange,
  compact = false,
  hiddenTypes,
}: SigmaGraphContainerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const internalSigmaRef = useRef<Sigma | null>(null);
  const layoutRef = useRef<FA2Layout | null>(null);
  // Incremented when sigma instance changes — used to trigger event handler registration.
  const [sigmaVersion, setSigmaVersion] = useState(0);
  const [hoveredNode, setHoveredNode] = useState<string | null>(null);
  // Pulse phase for animated highlighted edges (0..1, cycles)
  const [pulsePhase, setPulsePhase] = useState(0);
  // Camera ratio for edge density control (lower = more zoomed out)
  const [cameraRatio, setCameraRatio] = useState(1);
  // Layout running state
  const [layoutRunning, setLayoutRunning] = useState(false);
  const { isDark, labelColor, edgeColor, highlightEdgeColor } = useThemeColors();

  const setSigmaRef = useCallback(
    (instance: Sigma | null) => {
      internalSigmaRef.current = instance;
      setSigmaVersion((v) => v + 1);
      onSigmaReady?.(instance);
    },
    [onSigmaReady],
  );

  // Propagate layout state to parent
  useEffect(() => {
    onLayoutStateChange?.(layoutRunning);
  }, [layoutRunning, onLayoutStateChange]);

  /** Stop running layout worker — no post-processing to avoid visible flash.
   *  Noverlap + compactOrphans already ran after the sync pass (phase 1).
   *  The worker only does subtle refinement, so positions are already good. */
  const stopLayout = useCallback((_sigma: Sigma, _orphanRatio: number) => {
    const layout = layoutRef.current;
    if (layout?.isRunning()) layout.stop();
    layoutRef.current = null;
    setLayoutRunning(false);
  }, []);

  // --- Initialize Sigma + FA2 worker layout (non-blocking) ---
  useEffect(() => {
    if (!containerRef.current || graph.order === 0) return;

    // Random disc init — scale spread with canvas diagonal for responsive layout
    if (graph.order > 1) {
      const rect = containerRef.current.getBoundingClientRect();
      const canvasDiag = Math.sqrt(rect.width ** 2 + rect.height ** 2);
      const spread = Math.sqrt(graph.order) * (canvasDiag / 25);
      const nodes = graph.nodes();
      for (let i = 0; i < nodes.length; i++) {
        const angle = Math.random() * Math.PI * 2;
        const r = Math.sqrt(Math.random()) * spread;
        graph.setNodeAttribute(nodes[i], "x", Math.cos(angle) * r);
        graph.setNodeAttribute(nodes[i], "y", Math.sin(angle) * r);
      }
    }

    const edgePrograms: Record<string, typeof EdgeArrowProgram> = {
      arrow: EdgeArrowProgram,
      curvedArrow: EdgeCurvedArrowProgram as unknown as typeof EdgeArrowProgram,
    };

    // Create Sigma — shows nodes at random positions immediately
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
      zIndex: true,
      defaultDrawNodeHover: () => undefined,
    });

    setSigmaRef(sigma);

    // FA2 layout: sync rough pass first, then optional worker refinement.
    // This gives a reasonable layout immediately — no random-scatter animation.
    let timer: ReturnType<typeof setTimeout> | undefined;
    if (graph.order > 1) {
      let orphanCount = 0;
      const nodes = graph.nodes();
      for (const node of nodes) {
        if (graph.degree(node) === 0) orphanCount++;
      }
      const orphanRatio = orphanCount / graph.order;
      const { settings } = getFA2WorkerSettings(graph.order, orphanRatio);

      // Phase 1: quick sync pass — rough but instant layout
      const syncIters = graph.order < 200 ? 300 : graph.order < 1000 ? 100 : 50;
      forceAtlas2.assign(graph, { iterations: syncIters, settings });
      compactOrphans(graph);

      const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      if (compact || prefersReducedMotion) {
        // Compact/reduced-motion: sync-only, no worker
        if (orphanRatio < 0.3) {
          noverlap.assign(graph, {
            maxIterations: 30,
            settings: { margin: 2, ratio: 1.02, speed: 3, gridSize: 20 },
          });
        }
      } else {
        // Phase 2: worker refinement — subtle position tweaks, short duration
        const refineDuration = graph.order < 500 ? 1500 : graph.order < 2000 ? 2500 : 4000;
        const layout = new FA2Layout(graph, { settings });
        layoutRef.current = layout;
        setLayoutRunning(true);
        layout.start();

        timer = setTimeout(() => {
          stopLayout(sigma, orphanRatio);
        }, refineDuration);
      }
    }

    return () => {
      if (timer) clearTimeout(timer);
      if (layoutRef.current?.isRunning()) layoutRef.current.stop();
      layoutRef.current = null;
      setLayoutRunning(false);
      sigma.kill();
      if (internalSigmaRef.current === sigma) {
        internalSigmaRef.current = null;
        onSigmaReady?.(null);
      }
    };
  }, [graph, edgeType, compact]);

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
      const docType = getNodeType(data);
      const themedColor = getVaultNodeColor(docType, isDark);
      const themedData = { ...data, color: themedColor };

      // Filter: hide nodes of hidden types
      if (hiddenTypes?.size && hiddenTypes.has(docType)) {
        return { ...themedData, hidden: true };
      }

      // Active node neighborhood always visible regardless of zoom tier
      const activeNode = selectedNodeId || hoveredNode;
      const hood = neighborhoodRef.current;

      if (activeNode && hood) {
        if (node === activeNode) {
          return { ...themedData, zIndex: 3, forceLabel: true };
        }
        if (hood.nodes.has(node)) {
          return { ...themedData, zIndex: 2, forceLabel: true };
        }
      }

      // Semantic zoom: hide low-degree nodes when zoomed out
      const degree = graph.degree(node);
      if (cameraRatio > ZOOM_TIERS.FAR && degree < TIER_MIN_DEGREE.FAR) {
        return { ...themedData, hidden: true };
      }
      if (cameraRatio > ZOOM_TIERS.MID && degree < TIER_MIN_DEGREE.MID) {
        return { ...themedData, hidden: true };
      }

      if (activeNode && !hood) return themedData;
      if (!activeNode) return themedData;
      return { ...themedData, zIndex: 0 };
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

      // Active node neighborhood: always show related edges
      if (activeNode) {
        const hood = neighborhoodRef.current;
        if (!hood) return data;
        if (hood.edges.has(edge)) {
          const alpha = Math.round(200 + Math.sin(pulsePhase * Math.PI * 2) * 40);
          const alphaHex = Math.max(0, Math.min(255, alpha)).toString(16).padStart(2, "0");
          return { ...data, color: `${highlightEdgeColor}${alphaHex}`, size: 1, zIndex: 2 };
        }
        return { ...data, hidden: true };
      }

      // Semantic zoom edge culling (no active node).
      // Default/overview: NO edges. Edges appear progressively as user zooms in.
      const srcDegree = graph.degree(graph.source(edge));
      const tgtDegree = graph.degree(graph.target(edge));

      if (cameraRatio > ZOOM_TIERS.FAR) {
        // zoom ≤~170%: hide all edges
        return { ...data, hidden: true };
      }
      if (cameraRatio > ZOOM_TIERS.MID) {
        // zoom ~170-330%: only hub-to-hub
        if (srcDegree < TIER_EDGE_DEGREE.MID || tgtDegree < TIER_EDGE_DEGREE.MID) {
          return { ...data, hidden: true };
        }
        return { ...data, color: edgeColor, size: 0.3 };
      }
      if (cameraRatio > ZOOM_TIERS.NEAR) {
        // zoom ~330-830%: most edges
        if (srcDegree < TIER_EDGE_DEGREE.NEAR && tgtDegree < TIER_EDGE_DEGREE.NEAR) {
          return { ...data, hidden: true };
        }
        return { ...data, color: edgeColor, size: 0.4 };
      }
      // zoom >830%: show all edges
      return { ...data, color: edgeColor, size: 0.5 };
    });

    sigma.refresh();
    // sigmaVersion ensures this runs after sigma is created
  }, [selectedNodeId, hoveredNode, graph, highlightEdgeColor, edgeColor, hiddenTypes, cameraRatio, sigmaVersion, isDark]);

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

    // rAF-debounced camera handler with adaptive label density
    let pendingRatio = 1;
    let rafId = 0;
    const handleCameraUpdate = () => {
      pendingRatio = sigma.getCamera().ratio;
      if (!rafId) {
        rafId = requestAnimationFrame(() => {
          rafId = 0;
          setCameraRatio(pendingRatio);
          // Adaptive label density per zoom tier — fewer labels when zoomed out
          if (pendingRatio > ZOOM_TIERS.FAR) {
            // Default view: only biggest hubs get labels
            sigma.setSetting("labelRenderedSizeThreshold", 18);
            sigma.setSetting("labelDensity", 0.02);
          } else if (pendingRatio > ZOOM_TIERS.MID) {
            sigma.setSetting("labelRenderedSizeThreshold", 12);
            sigma.setSetting("labelDensity", 0.06);
          } else if (pendingRatio > ZOOM_TIERS.NEAR) {
            sigma.setSetting("labelRenderedSizeThreshold", 8);
            sigma.setSetting("labelDensity", 0.12);
          } else {
            sigma.setSetting("labelRenderedSizeThreshold", 5);
            sigma.setSetting("labelDensity", 0.2);
          }
        });
      }
    };

    sigma.on("enterNode", handleEnterNode);
    sigma.on("leaveNode", handleLeaveNode);
    sigma.on("clickNode", handleClickNode);
    sigma.on("doubleClickNode", handleDoubleClickNode);
    sigma.on("clickStage", handleClickStage);
    sigma.getCamera().on("updated", handleCameraUpdate);

    return () => {
      if (rafId) cancelAnimationFrame(rafId);
      sigma.off("enterNode", handleEnterNode);
      sigma.off("leaveNode", handleLeaveNode);
      sigma.off("clickNode", handleClickNode);
      sigma.off("doubleClickNode", handleDoubleClickNode);
      sigma.getCamera().off("updated", handleCameraUpdate);
      sigma.off("clickStage", handleClickStage);
    };
  }, [sigmaVersion]); // re-register only when sigma instance changes

  // NOTE: Click on node NO LONGER moves camera.
  // Camera only animates for explicit user actions (search, fit-to-view, keyboard F).
  // This matches the old force-graph behavior where clicking just highlights.

  const handleStopLayout = useCallback(() => {
    const sigma = internalSigmaRef.current;
    if (!sigma) return;
    let orphanCount = 0;
    const nodes = graph.nodes();
    for (const node of nodes) {
      if (graph.degree(node) === 0) orphanCount++;
    }
    stopLayout(sigma, orphanCount / (graph.order || 1));
  }, [graph, stopLayout]);

  // No-data state
  if (graph.order === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        No data to display
      </div>
    );
  }

  return (
    <div className="relative h-full w-full" style={{ minHeight: compact ? 200 : 300 }}>
      <div ref={containerRef} className="h-full w-full" />
      {layoutRunning && !compact && (
        <div className="absolute top-2 left-2 z-10 flex items-center gap-2">
          <div className="flex items-center gap-1.5 rounded-md bg-background/80 px-2 py-1 text-xs text-muted-foreground backdrop-blur-sm">
            <span className="h-2 w-2 animate-pulse rounded-full bg-blue-500" />
            Laying out…
          </div>
          <button
            onClick={handleStopLayout}
            className="rounded-md bg-background/80 px-2 py-1 text-xs text-muted-foreground backdrop-blur-sm hover:bg-muted cursor-pointer"
          >
            Stop
          </button>
        </div>
      )}
    </div>
  );
}
