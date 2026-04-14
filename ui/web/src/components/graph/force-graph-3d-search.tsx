import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import type Graph from "graphology";
import { Search, X } from "lucide-react";
import { Input } from "@/components/ui/input";
import { useUiStore } from "@/stores/use-ui-store";
import { getForceGraphNodeColor } from "@/adapters/graphology-to-force-graph";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type ForceGraphRef = any;

interface SearchResult {
  id: string;
  label: string;
  color: string;
  type: string;
}

interface ForceGraph3DSearchProps {
  graph: Graph;
  graphRef: ForceGraphRef | null;
  onNodeSelect?: (nodeId: string | null) => void;
  placeholder?: string;
  /** Hidden types to exclude from search results */
  hiddenTypes?: Set<string>;
}

export function ForceGraph3DSearch({
  graph,
  graphRef,
  onNodeSelect,
  placeholder = "Search nodes...",
  hiddenTypes,
}: ForceGraph3DSearchProps) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Theme for colors
  const theme = useUiStore((s) => s.theme);
  const isDark =
    theme === "dark" ||
    (theme === "system" &&
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);

  // Build search index from graph
  const searchIndex = useMemo(() => {
    const index: SearchResult[] = [];
    if (graph.order === 0) return index;
    graph.forEachNode((nodeId, attrs) => {
      const type = ((attrs.docType || attrs.entityType || "default") as string).toLowerCase();
      // Skip hidden types
      if (hiddenTypes?.has(type)) return;
      index.push({
        id: nodeId,
        label: (attrs.label as string) || nodeId,
        color: getForceGraphNodeColor(type, isDark),
        type,
      });
    });
    return index;
  }, [graph, hiddenTypes, isDark]);

  // Search graph nodes by label
  const handleSearch = useCallback(
    (q: string) => {
      setQuery(q);
      if (!q.trim() || searchIndex.length === 0) {
        setResults([]);
        setOpen(false);
        return;
      }
      const lower = q.toLowerCase();
      const matches = searchIndex
        .filter((item) => item.label.toLowerCase().includes(lower))
        .slice(0, 10);
      setResults(matches);
      setOpen(matches.length > 0);
      setActiveIndex(0);
    },
    [searchIndex]
  );

  // Select a result: highlight + camera zoom to node in 3D
  const selectResult = useCallback(
    (nodeId: string) => {
      onNodeSelect?.(nodeId);
      if (graphRef) {
        // Find node in graphData to get its 3D coordinates
        const graphData = graphRef.graphData?.();
        const node = graphData?.nodes?.find((n: { id: string }) => n.id === nodeId);
        if (node && node.x !== undefined && node.y !== undefined && node.z !== undefined) {
          const distance = 150;
          graphRef.cameraPosition(
            { x: node.x, y: node.y, z: node.z + distance },
            { x: node.x, y: node.y, z: node.z },
            1000
          );
        }
      }
      setOpen(false);
      setQuery("");
    },
    [graphRef, onNodeSelect]
  );

  // Keyboard navigation in dropdown
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!open || results.length === 0) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => Math.min(i + 1, results.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const r = results[activeIndex];
        if (r) selectResult(r.id);
      } else if (e.key === "Escape") {
        setOpen(false);
        inputRef.current?.blur();
      }
    },
    [open, results, activeIndex, selectResult]
  );

  // Close dropdown on outside click
  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, []);

  return (
    <div ref={containerRef} className="relative w-56">
      <div className="relative">
        <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground pointer-events-none" />
        <Input
          ref={inputRef}
          value={query}
          onChange={(e) => handleSearch(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => query && results.length > 0 && setOpen(true)}
          placeholder={placeholder}
          className="h-7 pl-7 pr-7 text-base md:text-xs"
          role="combobox"
          aria-expanded={open}
          aria-autocomplete="list"
          aria-controls="graph-3d-search-listbox"
          aria-label={placeholder}
        />
        {query && (
          <button
            onClick={() => {
              setQuery("");
              setResults([]);
              setOpen(false);
            }}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-muted"
            aria-label="Clear search"
          >
            <X className="h-3 w-3 text-muted-foreground" />
          </button>
        )}
      </div>

      {/* Dropdown results */}
      {open && (
        <div
          id="graph-3d-search-listbox"
          role="listbox"
          className="absolute top-full left-0 right-0 z-50 mt-1 rounded-md border bg-popover shadow-md max-h-60 overflow-y-auto"
        >
          {results.map((r, i) => (
            <button
              key={r.id}
              role="option"
              aria-selected={i === activeIndex}
              onClick={() => selectResult(r.id)}
              className={`flex items-center gap-2 w-full px-2.5 py-1.5 text-xs text-left hover:bg-accent ${
                i === activeIndex ? "bg-accent" : ""
              }`}
            >
              <span
                className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: r.color }}
              />
              <span className="truncate flex-1">{r.label}</span>
              <span className="text-2xs text-muted-foreground">{r.type}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
