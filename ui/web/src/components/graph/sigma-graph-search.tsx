import { useState, useRef, useCallback, useEffect } from "react";
import type Sigma from "sigma";
import type Graph from "graphology";
import { Search, X } from "lucide-react";
import { Input } from "@/components/ui/input";

interface SigmaGraphSearchProps {
  sigma: Sigma | null;
  graph: Graph;
  onNodeSelect?: (nodeId: string | null) => void;
  placeholder?: string;
}

interface SearchResult {
  id: string;
  label: string;
  color: string;
}

export function SigmaGraphSearch({ sigma, graph, onNodeSelect, placeholder = "Search nodes..." }: SigmaGraphSearchProps) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Search graph nodes by label
  const handleSearch = useCallback(
    (q: string) => {
      setQuery(q);
      if (!q.trim() || graph.order === 0) {
        setResults([]);
        setOpen(false);
        return;
      }
      const lower = q.toLowerCase();
      const matches: SearchResult[] = [];
      const nodes = graph.nodes();
      for (let i = 0; i < nodes.length && matches.length < 10; i++) {
        const node = nodes[i]!;
        const attrs = graph.getNodeAttributes(node);
        const label = (attrs.label as string) || "";
        if (label.toLowerCase().includes(lower)) {
          matches.push({ id: node, label, color: attrs.color as string });
        }
      }
      setResults(matches);
      setOpen(matches.length > 0);
      setActiveIndex(0);
    },
    [graph],
  );

  // Select a result: highlight + camera zoom
  const selectResult = useCallback(
    (nodeId: string) => {
      onNodeSelect?.(nodeId);
      if (sigma && graph.hasNode(nodeId)) {
        // Use getNodeDisplayData for correct stage coords (not raw graph coords).
        // Only zoom in if currently zoomed out (don't force zoom level if already zoomed in).
        const nodeDisplay = sigma.getNodeDisplayData(nodeId);
        if (nodeDisplay) {
          const currentRatio = sigma.getCamera().ratio;
          sigma.getCamera().animate(
            { x: nodeDisplay.x, y: nodeDisplay.y, ratio: Math.min(currentRatio, 0.5) },
            { duration: 400 },
          );
        }
      }
      setOpen(false);
      setQuery("");
    },
    [sigma, graph, onNodeSelect],
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
    [open, results, activeIndex, selectResult],
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
          aria-controls="graph-search-listbox"
          aria-label={placeholder}
        />
        {query && (
          <button
            onClick={() => { setQuery(""); setResults([]); setOpen(false); }}
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
          id="graph-search-listbox"
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
              <span className="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: r.color }} />
              <span className="truncate">{r.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
