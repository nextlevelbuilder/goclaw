import { useRef, useMemo, useState, useCallback, lazy, Suspense } from "react";
import type Sigma from "sigma";
import { useTranslation } from "react-i18next";
import type { KGEntity, KGRelation } from "@/types/knowledge-graph";
import { buildKGGraph, limitEntitiesByDegree, KG_TYPE_COLORS } from "@/adapters/kg-graph.adapter";
import { SigmaGraphContainer } from "@/components/graph/sigma-graph-container";
import { SigmaGraphControls } from "@/components/graph/sigma-graph-controls";
import { SigmaGraphSearch } from "@/components/graph/sigma-graph-search";
import { SigmaGraphFilters } from "@/components/graph/sigma-graph-filters";
import { SigmaGraphMinimap } from "@/components/graph/sigma-graph-minimap";
import { SigmaGraphKeyboardHelp } from "@/components/graph/sigma-graph-keyboard-help";
import { useSigmaKeyboard } from "@/components/graph/use-sigma-keyboard";
import { Square, Box } from "lucide-react";

// Lazy load 3D graph to reduce initial bundle size
const ForceGraph3DContainer = lazy(() =>
  import("@/components/graph/force-graph-3d-container").then((m) => ({ default: m.ForceGraph3DContainer }))
);
const ForceGraph3DSearch = lazy(() =>
  import("@/components/graph/force-graph-3d-search").then((m) => ({ default: m.ForceGraph3DSearch }))
);

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type ForceGraphRef = any;
type ViewMode = "2d" | "3d";

const DEFAULT_NODE_LIMIT = 2000;

interface KGGraphViewProps {
  entities: KGEntity[];
  relations: KGRelation[];
  onEntityClick?: (entity: KGEntity) => void;
  /** Compact mode for embedded mini-graphs (entity detail dialog) */
  compact?: boolean;
}

export function KGGraphView({ entities: allEntities, relations: allRelations, onEntityClick, compact = false }: KGGraphViewProps) {
  const { t } = useTranslation("memory");
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [sigma, setSigma] = useState<Sigma | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [nodeLimit, setNodeLimit] = useState(DEFAULT_NODE_LIMIT);
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(new Set());
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [viewMode, setViewMode] = useState<ViewMode>("2d");
  const [forceGraph3DRef, setForceGraph3DRef] = useState<ForceGraphRef>(null);

  const totalCount = allEntities.length;
  const isLimited = totalCount > nodeLimit;
  const entities = useMemo(
    () => limitEntitiesByDegree(allEntities, allRelations, nodeLimit),
    [allEntities, allRelations, nodeLimit],
  );
  const entityMap = useMemo(() => new Map(entities.map((e) => [e.id, e])), [entities]);
  const graph = useMemo(() => buildKGGraph(entities, allRelations), [entities, allRelations]);

  const handleNodeDoubleClick = useCallback((nodeId: string) => {
    const entity = entityMap.get(nodeId);
    if (entity) onEntityClick?.(entity);
  }, [entityMap, onEntityClick]);

  useSigmaKeyboard({
    sigma: compact ? null : sigma,
    graph,
    containerRef,
    selectedNodeId,
    onNodeSelect: setSelectedNodeId,
    searchInputRef,
  });

  const hasEntities = allEntities.length > 0;

  // Compact mode: simple graph only
  if (compact) {
    return (
      <div className="flex h-full flex-col rounded-md border overflow-hidden bg-background">
        <div className="min-h-0 flex-1 relative">
          {!hasEntities ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              {t("kg.graphView.empty")}
            </div>
          ) : (
            <SigmaGraphContainer
              graph={graph}
              edgeType="curvedArrow"
              selectedNodeId={selectedNodeId}
              onNodeSelect={setSelectedNodeId}
              onNodeDoubleClick={handleNodeDoubleClick}
              onSigmaReady={setSigma}
              compact
            />
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      tabIndex={0}
      role="application"
      aria-label={`Knowledge graph with ${totalCount} entities and ${allRelations.length} relations`}
      className="flex h-full flex-col rounded-md border overflow-hidden bg-background outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
    >
      {/* Top bar — responsive: legend stacks on mobile */}
      {hasEntities && (
        <div className="flex flex-col sm:flex-row sm:items-center gap-2 px-3 py-1 border-b shrink-0">
          {/* KG type legend */}
          <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground flex-1 min-w-0">
            {Object.entries(KG_TYPE_COLORS).map(([type, color]) => (
              <span key={type} className="flex items-center gap-1">
                <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />
                {type}
              </span>
            ))}
          </div>
          <div className="flex items-center gap-1 shrink-0 relative">
            {/* 2D/3D Toggle */}
            <div className="flex items-center rounded-md border bg-muted/50 p-0.5 mr-1">
              <button
                onClick={() => setViewMode("2d")}
                className={`flex items-center gap-1 rounded px-2 py-1 text-xs transition-colors ${
                  viewMode === "2d"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
                title="2D View"
              >
                <Square className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">2D</span>
              </button>
              <button
                onClick={() => setViewMode("3d")}
                className={`flex items-center gap-1 rounded px-2 py-1 text-xs transition-colors ${
                  viewMode === "3d"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
                title="3D View"
              >
                <Box className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">3D</span>
              </button>
            </div>
            {viewMode === "2d" ? (
              <SigmaGraphSearch
                sigma={sigma}
                graph={graph}
                onNodeSelect={setSelectedNodeId}
                placeholder={t("kg.graphView.search", { defaultValue: "Search entities..." })}
              />
            ) : (
              <Suspense fallback={<div className="w-56 h-7 bg-muted/50 rounded animate-pulse" />}>
                <ForceGraph3DSearch
                  graph={graph}
                  graphRef={forceGraph3DRef}
                  onNodeSelect={setSelectedNodeId}
                  placeholder={t("kg.graphView.search", { defaultValue: "Search entities..." })}
                  hiddenTypes={hiddenTypes}
                />
              </Suspense>
            )}
            <SigmaGraphFilters
              graph={graph}
              typeColors={KG_TYPE_COLORS}
              hiddenTypes={hiddenTypes}
              onHiddenTypesChange={setHiddenTypes}
              collapsed={!filtersOpen}
              onCollapsedChange={(c) => setFiltersOpen(!c)}
            />
            <SigmaGraphKeyboardHelp />
          </div>
        </div>
      )}

      {/* Graph canvas + minimap overlay */}
      <div className="min-h-0 flex-1 relative">
        {!hasEntities ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            {t("kg.graphView.empty")}
          </div>
        ) : viewMode === "2d" ? (
          <>
            <SigmaGraphContainer
              graph={graph}
              edgeType="curvedArrow"
              selectedNodeId={selectedNodeId}
              onNodeSelect={setSelectedNodeId}
              onNodeDoubleClick={handleNodeDoubleClick}
              onSigmaReady={setSigma}
              hiddenTypes={hiddenTypes}
            />
            <div className="absolute bottom-2 right-2 z-10 hidden sm:block">
              <SigmaGraphMinimap sigma={sigma} graph={graph} size={120} />
            </div>
          </>
        ) : (
          <Suspense fallback={<div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading 3D...</div>}>
            <ForceGraph3DContainer
              graph={graph}
              selectedNodeId={selectedNodeId}
              onNodeSelect={setSelectedNodeId}
              onNodeDoubleClick={handleNodeDoubleClick}
              hiddenTypes={hiddenTypes}
              onGraphRef={setForceGraph3DRef}
            />
          </Suspense>
        )}
      </div>

      {/* Stats bar */}
      <SigmaGraphControls
        sigma={sigma}
        nodeLimit={nodeLimit}
        isLimited={isLimited}
        onNodeLimitChange={setNodeLimit}
        labels={{
          nodes: t("kg.graphView.nodes", { count: totalCount }),
          edges: t("kg.graphView.edges", { count: allRelations.length }),
          limitNote: isLimited
            ? t("kg.graphView.limitNote", { limit: nodeLimit, total: totalCount })
            : undefined,
        }}
      />
    </div>
  );
}
