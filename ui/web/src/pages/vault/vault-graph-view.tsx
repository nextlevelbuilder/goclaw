import { useRef, useMemo, useState, useCallback } from "react";
import type Sigma from "sigma";
import { useTranslation } from "react-i18next";
import { buildVaultGraph, limitVaultDocsByDegree, VAULT_TYPE_COLORS } from "@/adapters/vault-graph-adapter";
import { SigmaGraphContainer } from "@/components/graph/sigma-graph-container";
import { SigmaGraphControls } from "@/components/graph/sigma-graph-controls";
import { SigmaGraphSearch } from "@/components/graph/sigma-graph-search";
import { SigmaGraphFilters } from "@/components/graph/sigma-graph-filters";
import { SigmaGraphMinimap } from "@/components/graph/sigma-graph-minimap";
import { SigmaGraphKeyboardHelp } from "@/components/graph/sigma-graph-keyboard-help";
import { useSigmaKeyboard } from "@/components/graph/use-sigma-keyboard";
import type { VaultDocument } from "@/types/vault";
import { useVaultGraphData } from "./hooks/use-vault";

const DEFAULT_NODE_LIMIT = 200;

interface Props {
  agentId: string;
  teamId?: string;
  selectedDocId?: string | null;
  onNodeSelect?: (docId: string | null) => void;
  onNodeDoubleClick?: (doc: VaultDocument) => void;
}

export function VaultGraphView({ agentId, teamId, selectedDocId, onNodeSelect, onNodeDoubleClick }: Props) {
  const { t } = useTranslation("vault");
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [sigma, setSigma] = useState<Sigma | null>(null);
  const [nodeLimit, setNodeLimit] = useState(DEFAULT_NODE_LIMIT);
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(new Set());
  const [filtersOpen, setFiltersOpen] = useState(false);

  const { documents: allDocs, links, loading } = useVaultGraphData(agentId, { teamId });

  const totalCount = allDocs.length;
  const isLimited = totalCount > nodeLimit;
  const documents = useMemo(() => limitVaultDocsByDegree(allDocs, links, nodeLimit), [allDocs, links, nodeLimit]);
  const docMap = useMemo(() => new Map(documents.map((d) => [d.id, d])), [documents]);
  // Only build graph when ALL data loaded — prevents double-render
  // (orphan-only layout → with-links layout) that causes visual chaos.
  const graph = useMemo(
    () => loading ? buildVaultGraph([], []) : buildVaultGraph(documents, links),
    [documents, links, loading],
  );

  const handleNodeDoubleClick = useCallback((nodeId: string) => {
    const doc = docMap.get(nodeId);
    if (doc) onNodeDoubleClick?.(doc);
  }, [docMap, onNodeDoubleClick]);

  useSigmaKeyboard({
    sigma,
    graph,
    containerRef,
    selectedNodeId: selectedDocId,
    onNodeSelect,
    searchInputRef,
  });

  const hasData = allDocs.length > 0;

  return (
    <div
      ref={containerRef}
      tabIndex={0}
      role="application"
      aria-label={`Vault knowledge graph with ${totalCount} documents and ${links.length} links`}
      className="flex h-full flex-col overflow-hidden bg-background outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
    >
      {/* Top bar — responsive: stacks on narrow screens */}
      <div className="flex flex-col sm:flex-row sm:items-center gap-2 px-3 py-1 border-b shrink-0">
        <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground flex-1 min-w-0">
          {Object.entries(VAULT_TYPE_COLORS).map(([type, color]) => (
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
              typeColors={VAULT_TYPE_COLORS}
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
        {loading && allDocs.length === 0 ? (
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
          nodes: t("graphDocs", { count: totalCount, defaultValue: "{{count}} docs" }),
          edges: t("graphLinks", { count: links.length, defaultValue: "{{count}} links" }),
          limitNote: isLimited
            ? t("graphLimitNote", { limit: nodeLimit, total: totalCount, defaultValue: "showing {{limit}} of {{total}}" })
            : undefined,
        }}
      />
    </div>
  );
}
