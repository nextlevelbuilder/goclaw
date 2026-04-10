import { useRef, useMemo, useState, useCallback } from "react";
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

const DEFAULT_NODE_LIMIT = 200;

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
            <SigmaGraphSearch
              sigma={sigma}
              graph={graph}
              onNodeSelect={setSelectedNodeId}
              placeholder={t("kg.graphView.search", { defaultValue: "Search entities..." })}
            />
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
        ) : (
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
