import { useRef, useCallback, useMemo, useEffect } from "react";
import ForceGraph3D from "react-force-graph-3d";
import type Graph from "graphology";
import { useUiStore } from "@/stores/use-ui-store";
import {
  graphologyToForceGraph,
  getForceGraphNodeColor,
  type ForceGraphNode,
} from "@/adapters/graphology-to-force-graph";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type ForceGraphRef = any;

export interface ForceGraph3DContainerProps {
  graph: Graph;
  selectedNodeId?: string | null;
  onNodeSelect?: (nodeId: string | null) => void;
  onNodeDoubleClick?: (nodeId: string) => void;
  /** Compact mode for embedded mini-graphs */
  compact?: boolean;
  /** Hidden node types for filtering */
  hiddenTypes?: Set<string>;
  /** Ref callback to expose ForceGraph3D instance for external control (e.g., search) */
  onGraphRef?: (ref: ForceGraphRef | null) => void;
}

/** Theme-aware colors */
function useThemeColors() {
  const theme = useUiStore((s) => s.theme);
  const systemDark =
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches;
  const isDark = theme === "dark" || (theme === "system" && systemDark);
  return {
    isDark,
    // Darker backgrounds for better node visibility
    backgroundColor: isDark ? "#0a0a0a" : "#f0f4f8",
    linkColor: isDark ? "#404040" : "#94a3b8",
    highlightLinkColor: isDark ? "#60a5fa" : "#3b82f6",
  };
}

export function ForceGraph3DContainer({
  graph,
  selectedNodeId,
  onNodeSelect,
  onNodeDoubleClick,
  compact = false,
  hiddenTypes,
  onGraphRef,
}: ForceGraph3DContainerProps) {
  const fgRef = useRef<ForceGraphRef>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const { isDark, backgroundColor, linkColor, highlightLinkColor } = useThemeColors();

  // Expose ref to parent via callback
  useEffect(() => {
    onGraphRef?.(fgRef.current);
    return () => onGraphRef?.(null);
  }, [onGraphRef]);

  // Convert Graphology to force-graph format and filter hidden types
  const graphData = useMemo(() => {
    const data = graphologyToForceGraph(graph);
    if (!hiddenTypes || hiddenTypes.size === 0) return data;

    // Filter out hidden node types
    const visibleNodeIds = new Set<string>();
    const filteredNodes = data.nodes.filter((node) => {
      const nodeType = (node.type || "default").toLowerCase();
      if (hiddenTypes.has(nodeType)) return false;
      visibleNodeIds.add(node.id);
      return true;
    });

    // Filter links to only include visible nodes
    const filteredLinks = data.links.filter(
      (link) => visibleNodeIds.has(link.source) && visibleNodeIds.has(link.target)
    );

    return { nodes: filteredNodes, links: filteredLinks };
  }, [graph, hiddenTypes]);

  // Compute node colors based on type and theme
  const nodeColors = useMemo(() => {
    const colors: Record<string, string> = {};
    for (const node of graphData.nodes) {
      colors[node.id] = getForceGraphNodeColor(node.type || "default", isDark);
    }
    return colors;
  }, [graphData.nodes, isDark]);

  // Node color getter
  const getNodeColor = useCallback(
    (node: ForceGraphNode) => {
      if (selectedNodeId === node.id) {
        return isDark ? "#fbbf24" : "#f59e0b"; // Highlight selected
      }
      return nodeColors[node.id] || (isDark ? "#a1a1aa" : "#71717a");
    },
    [nodeColors, selectedNodeId, isDark]
  );

  // Link color getter
  const getLinkColor = useCallback(
    (link: { source: ForceGraphNode; target: ForceGraphNode }) => {
      const sourceId = typeof link.source === "object" ? link.source.id : link.source;
      const targetId = typeof link.target === "object" ? link.target.id : link.target;
      if (selectedNodeId && (sourceId === selectedNodeId || targetId === selectedNodeId)) {
        return highlightLinkColor;
      }
      return linkColor;
    },
    [selectedNodeId, linkColor, highlightLinkColor]
  );

  // Node click handler
  const handleNodeClick = useCallback(
    (node: ForceGraphNode) => {
      if (onNodeSelect) {
        onNodeSelect(node.id === selectedNodeId ? null : node.id);
      }
      // Zoom to node
      if (fgRef.current && node.x !== undefined && node.y !== undefined && node.z !== undefined) {
        const distance = 150;
        fgRef.current.cameraPosition(
          { x: node.x, y: node.y, z: node.z + distance },
          { x: node.x, y: node.y, z: node.z },
          1000
        );
      }
    },
    [onNodeSelect, selectedNodeId]
  );

  // Node double-click handler
  const handleNodeDoubleClick = useCallback(
    (node: ForceGraphNode) => {
      if (onNodeDoubleClick) {
        onNodeDoubleClick(node.id);
      }
    },
    [onNodeDoubleClick]
  );

  // Background click handler
  const handleBackgroundClick = useCallback(() => {
    if (onNodeSelect) {
      onNodeSelect(null);
    }
  }, [onNodeSelect]);

  // Fit to view on mount
  useEffect(() => {
    if (fgRef.current && graphData.nodes.length > 0) {
      // Wait for layout to settle
      const timer = setTimeout(() => {
        fgRef.current?.zoomToFit(400, 50);
      }, 1500);
      return () => clearTimeout(timer);
    }
  }, [graphData.nodes.length]);

  // Handle container resize - ForceGraph3D auto-resizes with parent container

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
      className="relative h-full w-full"
      style={{ minHeight: compact ? 200 : 300 }}
    >
      <ForceGraph3D
        ref={fgRef}
        graphData={graphData as never}
        backgroundColor={backgroundColor}
        // Node styling - bigger nodes for visibility
        nodeColor={getNodeColor as never}
        nodeVal={(node: never) => ((node as ForceGraphNode).val || 5) * 2}
        nodeRelSize={6}
        nodeLabel={(node: never) => (node as ForceGraphNode).name}
        nodeOpacity={1}
        // Link styling - more visible
        linkColor={getLinkColor as never}
        linkWidth={1.5}
        linkOpacity={0.4}
        linkDirectionalParticles={2}
        linkDirectionalParticleWidth={1}
        linkDirectionalParticleSpeed={0.005}
        // Interactions
        onNodeClick={handleNodeClick as never}
        onNodeRightClick={handleNodeDoubleClick as never}
        onBackgroundClick={handleBackgroundClick}
        // Physics - tighter clustering
        cooldownTime={compact ? 1000 : 4000}
        d3AlphaDecay={0.02}
        d3VelocityDecay={0.4}
        // Performance
        enableNodeDrag={!compact}
        enableNavigationControls={true}
        showNavInfo={!compact}
      />
      {/* 3D indicator badge */}
      <div className="absolute top-2 right-2 z-10 rounded-md bg-background/80 px-2 py-1 text-xs font-medium text-muted-foreground backdrop-blur-sm">
        3D
      </div>
    </div>
  );
}
