import { useRef, useMemo, useState, useCallback } from "react";
import type Sigma from "sigma";
import { useTranslation } from "react-i18next";
import { buildVaultGraphFromDTO, VAULT_TYPE_COLORS_LIGHT, VAULT_TYPE_COLORS_DARK } from "@/adapters/vault-graph-adapter";
import { useUiStore } from "@/stores/use-ui-store";
import { SigmaGraphContainer } from "@/components/graph/sigma-graph-container";
import { SigmaGraphControls } from "@/components/graph/sigma-graph-controls";
import { SigmaGraphSearch } from "@/components/graph/sigma-graph-search";
import { SigmaGraphFilters } from "@/components/graph/sigma-graph-filters";
import { SigmaGraphMinimap } from "@/components/graph/sigma-graph-minimap";
import { SigmaGraphKeyboardHelp } from "@/components/graph/sigma-graph-keyboard-help";
import { useSigmaKeyboard } from "@/components/graph/use-sigma-keyboard";
import { useVaultGraphData } from "@/hooks/use-vault-graph-data";

const DEFAULT_NODE_LIMIT = 2000;

interface Props {
  agentId: string;
  teamId?: string;
  selectedDocId?: string | null;
  onNodeSelect?: (docId: string | null) => void;
  onNodeDoubleClick?: (nodeId: string) => void;
}

export function VaultGraphView({ agentId, teamId, selectedDocId, onNodeSelect, onNodeDoubleClick }: Props) {
  const { t } = useTranslation("vault");
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [sigma, setSigma] = useState<Sigma | null>(null);
  const [nodeLimit, setNodeLimit] = useState(DEFAULT_NODE_LIMIT);
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(new Set());
  const [filtersOpen, setFiltersOpen] = useState(false);

  // Theme-aware node colors — derive from store, not DOM class (avoids stale reads)
  const theme = useUiStore((s) => s.theme);
  const isDark = theme === "dark" || (theme === "system"
    && typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  const typeColors = isDark ? VAULT_TYPE_COLORS_DARK : VAULT_TYPE_COLORS_LIGHT;

  const { nodes, edges, totalNodes, totalEdges, loading } = useVaultGraphData(agentId, { teamId, limit: nodeLimit });

  const isLimited = totalNodes > nodeLimit;
  const nodeIds = useMemo(() => new Set(nodes.map((n) => n.id)), [nodes]);
  // Only build graph when data loaded — prevents double-render
  const graph = useMemo(
    () => loading ? buildVaultGraphFromDTO([], []) : buildVaultGraphFromDTO(nodes, edges),
    [nodes, edges, loading],
  );

  const handleNodeDoubleClick = useCallback((nodeId: string) => {
    if (nodeIds.has(nodeId)) onNodeDoubleClick?.(nodeId);
  }, [nodeIds, onNodeDoubleClick]);

  useSigmaKeyboard({
    sigma,
    graph,
    containerRef,
    selectedNodeId: selectedDocId,
    onNodeSelect,
    searchInputRef,
  });

  const hasData = nodes.length > 0;

  return (
    <div
      ref={containerRef}
      tabIndex={0}
      role="application"
      aria-label={`Vault knowledge graph with ${totalNodes} documents and ${totalEdges} links`}
      className="flex h-full flex-col overflow-hidden bg-background outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
    >
      {/* Top bar — responsive: stacks on narrow screens */}
      <div className="flex flex-col sm:flex-row sm:items-center gap-2 px-3 py-1 border-b shrink-0">
        <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground flex-1 min-w-0">
          {Object.entries(typeColors).map(([type, color]) => (
            <span key={type} className="flex items-center gap-1">
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />
              {type}
            </span>
          ))}
        </div>
        {hasData && (
          <div className="flex items-center gap-1 shrink-0 relative">
            <SigmaGraphSearch
              sigma={sigma}
              graph={graph}
              onNodeSelect={onNodeSelect}
              placeholder={t("graphSearch", { defaultValue: "Search docs..." })}
            />
            <SigmaGraphFilters
              graph={graph}
              typeColors={typeColors}
              hiddenTypes={hiddenTypes}
              onHiddenTypesChange={setHiddenTypes}
              collapsed={!filtersOpen}
              onCollapsedChange={(c) => setFiltersOpen(!c)}
            />
            <SigmaGraphKeyboardHelp />
          </div>
        )}
      </div>

      {/* Graph canvas + minimap overlay */}
      <div className="min-h-0 flex-1 relative">
        {loading && nodes.length === 0 ? (
          <div className="h-full animate-pulse rounded-md bg-muted" />
        ) : !hasData ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">No documents</div>
        ) : (
          <>
            <SigmaGraphContainer
              graph={graph}
              edgeType="curvedArrow"
              selectedNodeId={selectedDocId}
              onNodeSelect={onNodeSelect}
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
          nodes: t("graphDocs", { count: totalNodes, defaultValue: "{{count}} docs" }),
          edges: t("graphLinks", { count: totalEdges, defaultValue: "{{count}} links" }),
          limitNote: isLimited
            ? t("graphLimitNote", { limit: nodeLimit, total: totalNodes, defaultValue: "showing {{limit}} of {{total}}" })
            : undefined,
        }}
      />
    </div>
  );
}
